package imageimport

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/storage"
)

func TestReadPicks(t *testing.T) {
	const head = "category,type_key,commons_file,page_url,licence,attribution\n"
	for _, c := range []struct {
		name string
		in   string
		want []Pick
		err  string
	}{
		{"saved list", head + "staples,toor dal,Toor dal.jpg,https://x,CC0,Someone\nhousehold,garbage bags medium,File:Garbage bag.jpg,,,\n",
			[]Pick{{"staples", "toor dal", "Toor dal.jpg"}, {"household", "garbage bags medium", "Garbage bag.jpg"}}, ""},
		{"three columns and a BOM", "\uFEFFcategory,type_key,commons_file\nstaples, besan ,Gram flour.jpg\n",
			[]Pick{{"staples", "besan", "Gram flour.jpg"}}, ""},
		{"header only", head, nil, ""},
		{"wrong header", "kind,file\ntoor dal,Toor dal.jpg\n", nil, "header must start"},
		{"empty", "", nil, "header"},
		{"short line", head + "staples,toor dal\n", nil, "line 2"},
		{"blank file", head + "staples,toor dal,\n", nil, "line 2: category, type_key and commons_file are required"},
		{"twice", head + "staples,toor dal,A.jpg\nstaples,toor dal,B.jpg\n", nil, "line 3: toor dal in staples is picked twice"},
	} {
		got, err := ReadPicks(strings.NewReader(c.in))
		if c.err != "" {
			require.ErrorContains(t, err, c.err, c.name)
			continue
		}
		require.NoError(t, err, c.name)
		require.Equal(t, c.want, got, c.name)
	}

	// The founder's list compiled in: 156 kinds, each once.
	picks, err := ReadPicks(bytes.NewReader(BuiltinPicks))
	require.NoError(t, err)
	require.Len(t, picks, 156)
}

func TestTypesPicks(t *testing.T) {
	w := newWorld(t)
	w.product("Tata Sampann Toor Dal 1 kg", "Tata Sampann", catStaples)
	w.product("Toor Dal 500 g", "", catStaples)
	w.product("Tata Salt 1 kg", "Tata", catStaples)
	w.product("Fortune Besan 500 g", "Fortune", catStaples)
	w.product("Aashirvaad Atta 5 kg", "Aashirvaad", catStaples)
	w.product("Daawat Basmati Rice 1 kg", "Daawat", catStaples)
	butter := w.product("Amul Buttermilk 500 ml", "Amul", catDairy)
	w.product("Shalimar Garbage Bags Medium 30 pcs", "Shalimar", catHousehold)

	photo := func(cat, kind, status, ref string, extra ...string) ids.ID {
		id := ids.New(t0)
		key := "type-photos/" + id.String() + ".jpg"
		var public any
		if status == "approved" {
			public = "types/" + id.String() + ".jpg"
		}
		w.exec(`INSERT INTO type_photos (id, category_id, type_key, status, rank, object_key, content_type, size_bytes, public_key, source_ref, source_url, page_url, licence, attribution)
		  VALUES ($1, $2, $3, $4, 1, $5, 'image/jpeg', 10, $6, $7, 'https://x', 'https://y', 'CC0', 'Someone')`,
			ids.UUID(id), cat, kind, status, key, public, ref)
		if len(extra) > 0 {
			w.fake.PutPrivate(key, "image/jpeg", []byte(extra[0]))
		}
		return id
	}
	// Buttermilk already has its photo, and a product that came later.
	photo(catDairy, "buttermilk", "approved", "Buttermilk.jpg")
	// Besan's picked file is already a candidate (with its copy), next to another.
	besan := photo(catStaples, "besan", "pending", "Gram flour.jpg", "besan")
	besanOther := photo(catStaples, "besan", "pending", "Besan ladoo.jpg", "x")
	// Atta's picked file was turned down in A-08.
	photo(catStaples, "atta", "rejected", "Wheat flour.jpg")
	// Toor dal has a candidate nobody looked at; its pick is fetched.
	toorOther := photo(catStaples, "toor dal", "pending", "Pigeon pea.jpg", "x")

	commons := newFakeCommons(t, nil)
	commons.licences["Salt.jpg"] = "CC BY-NC 4.0"
	commons.gone["Basmati rice.jpg"] = true
	picks := []Pick{
		{"staples", "toor dal", "Split pigeon peas.jpg"},
		{"staples", "besan", "Gram flour.jpg"},
		{"staples", "atta", "Wheat flour.jpg"},
		{"staples", "salt", "Salt.jpg"},
		{"staples", "basmati rice", "Basmati rice.jpg"},
		{"staples", "jaggery", "Jaggery.jpg"},
		{"dairy-eggs", "buttermilk", "Another buttermilk.jpg"},
		{"nowhere", "garbage bags medium", "Garbage bag.jpg"},
	}

	u, err := url.Parse(commons.files.URL)
	require.NoError(t, err)
	importer := func(dry bool) *TypeImporter {
		ti, err := NewTypes(Deps{
			DB: db.New(w.tdb.Pool), Store: storage.New(w.fake.Config(), w.fake.Client()), Client: httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: true, Timeout: 5 * time.Second}),
			Audit: audit.New(db.New(w.tdb.Pool), w.clk, nil), Clock: w.clk, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
			Sleep: func(_ context.Context, d time.Duration) error {
				w.slept = append(w.slept, d)
				w.clk.Advance(d)
				return nil
			},
		}, TypeOptions{UserAgent: UserAgent("test", "https://todayz.in"), Limit: 1, DryRun: dry, APIURL: commons.api.URL, FileHosts: []string{u.Host}})
		require.NoError(t, err)
		return ti
	}

	// A dry run checks each new file on Commons and stores nothing.
	rep, err := importer(true).Picks(context.Background(), picks)
	require.NoError(t, err)
	require.Len(t, rep.Picked, 3) // toor dal, besan, jaggery
	require.EqualValues(t, 5, w.scalar(`SELECT count(*) FROM type_photos`))
	require.Empty(t, w.fake.Images())

	commons.searches = nil
	rep, err = importer(false).Picks(context.Background(), picks)
	require.NoError(t, err)
	require.Equal(t, 1, rep.Filled, "buttermilk's photo went to the product added since")

	outcome := map[string]TypeLine{}
	for _, set := range []struct {
		name  string
		lines []TypeLine
	}{{"picked", rep.Picked}, {"kept", rep.Kept}, {"failed", rep.Failed}} {
		for _, l := range set.lines {
			l.Reason = set.name + ": " + l.Reason
			outcome[l.TypeKey] = l
		}
	}
	for _, c := range []struct {
		kind     string
		reason   string
		products int
	}{
		{"toor dal", "picked: ", 2},
		{"besan", "picked: ", 1},
		{"jaggery", "picked: ", 0}, // no product yet; a later run fills them
		{"atta", "kept: the file was turned down in A-08", 0},
		{"buttermilk", "kept: the kind already has a photo", 0},
		{"salt", `failed: listed on Commons under "CC BY-NC 4.0"`, 0},
		{"basmati rice", "failed: no such file on Commons", 0},
		{"garbage bags medium", `failed: no category "nowhere"`, 0},
	} {
		got, ok := outcome[c.kind]
		require.True(t, ok, c.kind)
		require.True(t, strings.HasPrefix(got.Reason, c.reason), "%s: %s", c.kind, got.Reason)
		require.Equal(t, c.products, got.Products, c.kind)
	}

	// Only files the database did not have were looked up, one call at a time.
	require.ElementsMatch(t, []string{"titles:Split pigeon peas.jpg", "titles:Salt.jpg", "titles:Basmati rice.jpg", "titles:Jaggery.jpg"}, commons.searches)

	// Each pick is approved as A-08 does it, by the system: a shared public
	// copy, the other candidates turned down, the products given the photo.
	var toor ids.ID
	for _, kind := range []string{"toor dal", "besan", "jaggery"} {
		var u pgtype.UUID
		var public, by any
		require.NoError(t, w.tdb.OwnerPool.QueryRow(context.Background(),
			`SELECT id, public_key, decided_by FROM type_photos WHERE type_key = $1 AND status = 'approved'`, kind).Scan(&u, &public, &by))
		id, _ := ids.FromUUID(u)
		require.Equal(t, "types/"+id.String()+".jpg", public, kind)
		require.Nil(t, by, kind)
		_, _, ok := w.fake.Image(public.(string))
		require.True(t, ok, kind)
		switch kind {
		case "toor dal":
			toor = id
		case "besan":
			require.Equal(t, besan, id, "besan's own candidate is the one approved")
		}
	}
	for _, id := range []ids.ID{besanOther, toorOther} {
		require.Equal(t, otherPicked, w.scalar(`SELECT reject_reason FROM type_photos WHERE id = $1`, ids.UUID(id)))
	}
	require.EqualValues(t, 2, w.scalar(`SELECT count(*) FROM products p JOIN product_images i ON i.id = p.image_id
	  WHERE i.source = 'type' AND i.type_photo_id = $1 AND p.image_key = $2`, ids.UUID(toor), "types/"+toor.String()+".jpg"))
	require.NotNil(t, w.scalar(`SELECT image_id FROM products WHERE id = $1`, ids.UUID(butter)))
	require.EqualValues(t, 3, w.scalar(`SELECT count(*) FROM audit_log WHERE actor_kind = 'system' AND actor_id IS NULL
	  AND action = 'catalogue.type_photo.approve' AND subject_type = 'type_photo'`))
	require.Equal(t, "images types --picks", w.scalar(`SELECT after ->> 'via' FROM audit_log WHERE subject_id = $1`, toor.String()))
	for i := 1; i < len(w.slept); i++ {
		require.LessOrEqual(t, w.slept[i], CommonsPace)
	}

	// Again: the picked kinds are left alone and nothing new is stored.
	rows := w.scalar(`SELECT count(*) FROM type_photos`)
	rep, err = importer(false).Picks(context.Background(), picks)
	require.NoError(t, err)
	require.Empty(t, rep.Picked)
	require.Len(t, rep.Kept, 5)
	require.Len(t, rep.Failed, 3)
	require.Equal(t, rows, w.scalar(`SELECT count(*) FROM type_photos`))
}
