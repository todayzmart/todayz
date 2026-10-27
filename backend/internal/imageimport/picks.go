package imageimport

import (
	"context"
	_ "embed" // the saved picks
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/todayz/backend/gen/sqlc/imageimportdb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/storage"
)

// Saved picks (D-265, `todayz images types --picks FILE`). The founder's
// picks in A-08 live only in type_photos, so a reset or a new database loses
// them. A pick list names, per kind, the Commons file that was picked; for
// each kind with no approved photo the run fetches that exact file (paced,
// with the same licence and size checks as a search), stores it as a
// candidate and approves it with the steps of admin's approveTypePhoto:
// publish to types/<id>.<ext>, turn the kind's other candidates down, give
// the kind's products the photo, and audit it, with the system as the actor.
// A kind that already has a photo is left alone, and so is a file that was
// turned down in A-08 (a take-down outranks the list).

// BuiltinPicks are the founder's picks, saved on 27 Oct (`--picks builtin`).
//
//go:embed data/approved-type-photos.csv
var BuiltinPicks []byte

// pickHeader are the columns a pick list starts with. Any after them (the
// file page, licence and author when it was picked) are for people; the run
// takes those from Commons as it is now.
var pickHeader = []string{"category", "type_key", "commons_file"}

// otherPicked is the reason the kind's other candidates are turned down
// (admin's approveTypePhoto uses the same words).
const otherPicked = "another photo was picked for this kind"

// Pick is one kind's chosen Commons file.
type Pick struct {
	Category string // category slug
	TypeKey  string // item_type_key, as type_photos has it
	File     string // Commons file name, without "File:"
}

// ReadPicks reads a pick list: a CSV whose header starts category, type_key,
// commons_file, with one row per kind.
func ReadPicks(r io.Reader) ([]Pick, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	head, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("imageimport: picks header: %w", err)
	}
	if len(head) < len(pickHeader) {
		return nil, fmt.Errorf("imageimport: picks header must start %s", strings.Join(pickHeader, ","))
	}
	for i, h := range pickHeader {
		if strings.TrimSpace(strings.TrimPrefix(head[i], "\uFEFF")) != h {
			return nil, fmt.Errorf("imageimport: picks header must start %s", strings.Join(pickHeader, ","))
		}
	}
	var out []Pick
	seen := map[[2]string]bool{}
	for line := 2; ; line++ {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("imageimport: picks: %w", err)
		}
		if len(rec) < len(pickHeader) {
			return nil, fmt.Errorf("imageimport: picks line %d: want %d columns", line, len(pickHeader))
		}
		p := Pick{Category: strings.TrimSpace(rec[0]), TypeKey: strings.TrimSpace(rec[1]), File: strings.TrimSpace(strings.TrimPrefix(rec[2], "File:"))}
		if p.Category == "" || p.TypeKey == "" || p.File == "" {
			return nil, fmt.Errorf("imageimport: picks line %d: category, type_key and commons_file are required", line)
		}
		k := [2]string{p.Category, p.TypeKey}
		if seen[k] {
			return nil, fmt.Errorf("imageimport: picks line %d: %s in %s is picked twice", line, p.TypeKey, p.Category)
		}
		seen[k] = true
		out = append(out, p)
	}
}

// PickReport is what applying a pick list did.
type PickReport struct {
	Filled int        // products given an approved kind's photo before the picks
	Picked []TypeLine // applied (or, on a dry run, would be); Products took the photo now
	Kept   []TypeLine // left alone: the kind has a photo, or the file was turned down
	Failed []TypeLine
	DryRun bool
}

// Picks applies a pick list. It first gives approved kinds' photos to
// products that lack one (as Run does), so running it again after products
// are added fills them too.
func (t *TypeImporter) Picks(ctx context.Context, picks []Pick) (PickReport, error) {
	rep := PickReport{DryRun: t.o.DryRun}
	if !t.o.DryRun {
		if t.d.Audit == nil {
			return rep, errors.New("imageimport: picks need an audit writer")
		}
		n, err := t.fill(ctx)
		rep.Filled = n
		if err != nil {
			return rep, err
		}
	}
	for _, p := range picks {
		line := TypeLine{Category: p.Category, TypeKey: p.TypeKey, Files: []string{p.File}}
		kept, n, err := t.pick(ctx, p)
		line.Products = n
		switch {
		case err != nil && stopping(ctx, err):
			return rep, err
		case err != nil:
			line.Reason = err.Error()
			rep.Failed = append(rep.Failed, line)
			t.d.Log.WarnContext(ctx, "type photo pick failed", slog.String("type_key", p.TypeKey), slog.String("error", err.Error()))
		case kept != "":
			line.Reason = kept
			rep.Kept = append(rep.Kept, line)
		default:
			rep.Picked = append(rep.Picked, line)
		}
	}
	return rep, nil
}

// pick applies one pick. It returns why the kind was left alone (or ""), and
// how many products took the photo.
func (t *TypeImporter) pick(ctx context.Context, p Pick) (string, int, error) {
	q := imageimportdb.New(t.d.DB.Q(ctx))
	cat, err := q.CategoryBySlug(ctx, p.Category)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, fmt.Errorf("no category %q", p.Category)
	}
	if err != nil {
		return "", 0, fmt.Errorf("imageimport: category: %w", err)
	}
	has, err := q.TypeKindHasApproved(ctx, imageimportdb.TypeKindHasApprovedParams{CategoryID: cat, TypeKey: p.TypeKey})
	if err != nil {
		return "", 0, fmt.Errorf("imageimport: kind photo: %w", err)
	}
	if has {
		return "the kind already has a photo", 0, nil
	}
	id, kept, err := t.candidate(ctx, cat, p)
	if err != nil || kept != "" || t.o.DryRun {
		return kept, 0, err
	}
	n, err := t.approve(ctx, id)
	return "", n, err
}

// candidate finds the kind's row for the picked file, or fetches the file
// from Commons and stores it as a candidate. On a dry run it only checks the
// file.
func (t *TypeImporter) candidate(ctx context.Context, cat pgtype.UUID, p Pick) (ids.ID, string, error) {
	q := imageimportdb.New(t.d.DB.Q(ctx))
	row, err := q.TypeKindFile(ctx, imageimportdb.TypeKindFileParams{CategoryID: cat, TypeKey: p.TypeKey, SourceRef: p.File})
	switch {
	case err == nil && row.Status == tpRejected:
		return ids.Zero, "the file was turned down in A-08", nil
	case err == nil:
		id, _ := ids.FromUUID(row.ID)
		return id, "", nil
	case !errors.Is(err, pgx.ErrNoRows):
		return ids.Zero, "", fmt.Errorf("imageimport: kind file: %w", err)
	}
	v := url.Values{}
	v.Set("titles", "File:"+p.File)
	files, err := t.imageInfo(ctx, v)
	if err != nil {
		return ids.Zero, "", err
	}
	if len(files) == 0 {
		return ids.Zero, "", errors.New("no such file on Commons")
	}
	f := files[0]
	if why := unusable(f); why != "" {
		return ids.Zero, "", errors.New(why)
	}
	if t.o.DryRun {
		return ids.Zero, "", nil
	}
	id, ok, err := t.store(ctx, cat, p.TypeKey, f, 1)
	if err != nil {
		return ids.Zero, "", err
	}
	if !ok {
		return ids.Zero, "", errors.New("the kind got this file while it was being copied; run again")
	}
	return id, "", nil
}

// unusable says why a picked file may not be shown now (its licence changed,
// it was marked restricted, or it is no photo we take), or "".
func unusable(f commonsFile) string {
	switch {
	case usable(f):
		return ""
	case !CommonsLicences[f.Licence]:
		return fmt.Sprintf("listed on Commons under %q, not a licence we take", f.Licence)
	case f.Restrict != "":
		return "marked restricted on Commons: " + f.Restrict
	}
	return "not a usable photo (type, size or credit)"
}

const tpRejected = "rejected"

// approve picks a candidate as admin's approveTypePhoto does, in one
// transaction with its audit entry. It returns how many products took it.
func (t *TypeImporter) approve(ctx context.Context, id ids.ID) (int, error) {
	n := 0
	err := t.d.DB.InTx(ctx, func(ctx context.Context) error {
		q := imageimportdb.New(t.d.DB.Q(ctx))
		tp, err := q.LockTypePhoto(ctx, ids.UUID(id))
		if err != nil {
			return fmt.Errorf("imageimport: lock type photo: %w", err)
		}
		if tp.Status != "pending" {
			return errors.New("the photo is not waiting for review")
		}
		has, err := q.TypeKindHasApproved(ctx, imageimportdb.TypeKindHasApprovedParams{CategoryID: tp.CategoryID, TypeKey: tp.TypeKey})
		if err != nil {
			return err
		}
		if has {
			return errors.New("this kind already has a photo")
		}
		now := t.d.Clock.Now()
		key := "types/" + id.String() + ext(tp.ContentType)
		err = t.d.Store.Publish(ctx, tp.ObjectKey, key, tp.ContentType, maxImageBytes, now)
		switch {
		case errors.Is(err, storage.ErrNoObject):
			return errors.New("the stored copy is missing; drop it with --again, then apply the picks again")
		case errors.Is(err, storage.ErrTooLarge):
			return errors.New("the stored copy is larger than 5 MB")
		case err != nil:
			return fmt.Errorf("imageimport: publish type photo: %w", err)
		}
		if err := q.ApproveTypePhoto(ctx, imageimportdb.ApproveTypePhotoParams{PublicKey: &key, Now: now, ID: tp.ID}); err != nil {
			return err
		}
		reason := otherPicked
		if _, err := q.RejectOtherTypePhotos(ctx, imageimportdb.RejectOtherTypePhotosParams{
			Reason: &reason, Now: now, CategoryID: tp.CategoryID, TypeKey: tp.TypeKey, ID: tp.ID,
		}); err != nil {
			return err
		}
		if n, err = t.fillKind(ctx, q, tp.ID, tp.CategoryID, tp.TypeKey, key); err != nil {
			return err
		}
		_, err = t.d.Audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorSystem, Action: "catalogue.type_photo.approve",
			SubjectType: "type_photo", SubjectID: id.String(),
			Before: map[string]any{"status": tp.Status, "typeKey": tp.TypeKey},
			After:  map[string]any{"status": "approved", "imageKey": key, "products": n, "via": "images types --picks"},
		})
		return err
	})
	return n, err
}
