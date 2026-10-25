package business

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5/pgtype"

	businessapi "github.com/todayz/backend/gen/api/business"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/businessdb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// maxDocuments is how many documents the list returns, newest first. A
// business uploads a handful (PAN, GST, a few photos); the contract has no
// paging for it.
const maxDocuments = 100 //settings:ok a page size, not the delivery radius minimum

// maxDocumentBytes is the largest document upload; the presigned PUT signs the
// declared size, so the object cannot be larger.
//
// TODO(lead): 01 has no size rule for business documents; 10 MB is the
// review's default (a phone photo or a scanned PDF fits). The contract's
// DocumentCreate.sizeBytes maximum says the same.
const maxDocumentBytes = 10 << 20

// maxDocumentsPerKind caps the documents of one kind a business holds that are
// not rejected (pending included, so unused upload URLs cannot pile up
// objects).
//
// TODO(lead): 01 has no rule; 5 is the review's small default. Shop photos
// of a business with many stores may need more (or a cap per store).
const maxDocumentsPerKind = 5

// errNoStorage is why an upload URL cannot be made.
var errNoStorage = errors.New("business: storage is not configured")

func toDocument(d businessdb.BusinessDocument) businessapi.BusinessDocument {
	return businessapi.BusinessDocument{
		ContentType:  businessapi.DocumentContentType(d.ContentType),
		CreatedAt:    d.CreatedAt.UTC(),
		Id:           idString(d.ID),
		Kind:         businessapi.DocumentKind(d.Kind),
		RejectReason: d.RejectReason,
		Status:       businessapi.DocumentStatus(d.Status),
		StoreId:      optIDString(d.StoreID),
	}
}

func (s *Service) listDocuments(ctx context.Context, bidStr string) (businessapi.DocumentList, error) {
	if err := s.ready(ctx); err != nil {
		return businessapi.DocumentList{}, err
	}
	bid, err := pathID(bidStr)
	if err != nil {
		return businessapi.DocumentList{}, err
	}
	rows, err := s.q(ctx).ListDocuments(ctx, businessdb.ListDocumentsParams{BusinessID: ids.UUID(bid), MaxRows: maxDocuments})
	if err != nil {
		return businessapi.DocumentList{}, fmt.Errorf("business: documents: %w", err)
	}
	out := businessapi.DocumentList{Items: make([]businessapi.BusinessDocument, 0, len(rows))}
	for _, d := range rows {
		out.Items = append(out.Items, toDocument(d))
	}
	return out, nil
}

// createDocument records a pending document and returns a presigned PUT for
// it. The object key is never shown; the URL is valid for uploadTTL and signs
// the Content-Type and Content-Length, so the upload must be the declared type
// and size.
//
// TODO(lead): nothing turns 'pending' into 'uploaded' yet. Either a confirm
// call (HEAD the object, check its size and type) or the admin review (W1-10)
// must check the object exists before trusting the row.
func (s *Service) createDocument(ctx context.Context, bidStr string, body *businessapi.DocumentCreate) (businessapi.DocumentUpload, error) {
	if err := s.ready(ctx); err != nil {
		return businessapi.DocumentUpload{}, err
	}
	if s.presign == nil {
		return businessapi.DocumentUpload{}, problem.Wrap(genproblems.ServiceUnavailable, errNoStorage)
	}
	p, err := principal(ctx)
	if err != nil {
		return businessapi.DocumentUpload{}, err
	}
	bid, err := pathID(bidStr)
	if err != nil {
		return businessapi.DocumentUpload{}, err
	}
	if body == nil {
		return businessapi.DocumentUpload{}, problem.New(genproblems.BadRequest, "")
	}
	var v invalid
	store := optUUID(&v, "body.storeId", body.StoreId)
	if body.SizeBytes < 1 || body.SizeBytes > maxDocumentBytes {
		v.add("body.sizeBytes", "must be between 1 byte and 10 MB")
	}
	if err := v.err(); err != nil {
		return businessapi.DocumentUpload{}, err
	}
	doc, key, err := s.insertDocument(ctx, audit.ActorStaff, p.UserID, bid, store, string(body.Kind), string(body.ContentType), body.SizeBytes, nil)
	if err != nil {
		return businessapi.DocumentUpload{}, err
	}
	now := s.clock.Now()
	return businessapi.DocumentUpload{
		Document: toDocument(doc),
		Upload: businessapi.DocumentUploadTarget{
			ExpiresAt: now.Add(uploadTTL).UTC(),
			Headers: businessapi.DocumentUploadHeaders{
				ContentType: body.ContentType, ContentLength: strconv.FormatInt(doc.SizeBytes, 10),
			},
			Method: businessapi.PUT,
			Url:    s.presign.putURL(key, string(body.ContentType), doc.SizeBytes, now),
		},
	}, nil
}

// insertDocument records a pending document of the business (kind, content
// type and size already checked) and returns it with its object key, audited
// as the actor (kind, uid). check, when set, runs first in the transaction.
func (s *Service) insertDocument(ctx context.Context, actorKind string, uid, bid ids.ID, store pgtype.UUID, kind, contentType string, size int64, check func(context.Context) error) (businessdb.BusinessDocument, string, error) {
	var doc businessdb.BusinessDocument
	var key string
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		if check != nil {
			if err := check(ctx); err != nil {
				return err
			}
		}
		if store.Valid {
			owner, err := q.StoreBusiness(ctx, store)
			if db.IsNoRows(err) || (err == nil && owner != ids.UUID(bid)) {
				return problem.New(genproblems.ValidationFailed, "").WithField("body.storeId", "must be a store of this business")
			}
			if err != nil {
				return err
			}
		}
		// One business and kind at a time, so two requests cannot both pass
		// the cap.
		if err := q.LockKey(ctx, "document:"+bid.String()+":"+kind); err != nil {
			return err
		}
		n, err := q.CountDocumentsOfKind(ctx, businessdb.CountDocumentsOfKindParams{BusinessID: ids.UUID(bid), Kind: kind})
		if err != nil {
			return err
		}
		if n >= maxDocumentsPerKind {
			return problem.New(genproblems.Conflict, "this business already has the most documents of this kind")
		}
		now := s.clock.Now()
		id := ids.New(now)
		key = "businesses/" + bid.String() + "/documents/" + id.String()
		doc, err = q.InsertDocument(ctx, businessdb.InsertDocumentParams{
			ID: ids.UUID(id), BusinessID: ids.UUID(bid), StoreID: store, Kind: kind,
			ObjectKey: key, ContentType: contentType, SizeBytes: size, Now: now,
		})
		if err != nil {
			return err
		}
		return s.writeAuditAs(ctx, actorKind, uid, "document.create", "business_document", id.String(), nil,
			map[string]any{"kind": doc.Kind, "contentType": doc.ContentType, "sizeBytes": doc.SizeBytes, "storeId": optIDString(doc.StoreID)})
	})
	return doc, key, err
}
