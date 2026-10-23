package business

import (
	"context"
	"errors"
	"fmt"

	businessapi "github.com/todayz/backend/gen/api/business"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/businessdb"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// maxDocuments is how many documents the list returns, newest first. A
// business uploads a handful (PAN, GST, a few photos); the contract has no
// paging for it.
const maxDocuments = 100 //settings:ok a page size, not the delivery radius minimum

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

// createDocument records a document and returns a presigned PUT for it. The
// object key is never shown; the URL is valid for uploadTTL and signs the
// Content-Type, so the upload must be the declared type.
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
	if err := v.err(); err != nil {
		return businessapi.DocumentUpload{}, err
	}

	var doc businessdb.BusinessDocument
	var key string
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		if store.Valid {
			owner, err := q.StoreBusiness(ctx, store)
			if db.IsNoRows(err) || (err == nil && owner != ids.UUID(bid)) {
				return problem.New(genproblems.ValidationFailed, "").WithField("body.storeId", "must be a store of this business")
			}
			if err != nil {
				return err
			}
		}
		now := s.clock.Now()
		id := ids.New(now)
		key = "businesses/" + bid.String() + "/documents/" + id.String()
		doc, err = q.InsertDocument(ctx, businessdb.InsertDocumentParams{
			ID: ids.UUID(id), BusinessID: ids.UUID(bid), StoreID: store,
			Kind: string(body.Kind), ObjectKey: key, ContentType: string(body.ContentType), Now: now,
		})
		if err != nil {
			return err
		}
		return s.writeAudit(ctx, p.UserID, "document.create", "business_document", id.String(), nil,
			map[string]any{"kind": doc.Kind, "contentType": doc.ContentType, "storeId": optIDString(doc.StoreID)})
	})
	if err != nil {
		return businessapi.DocumentUpload{}, err
	}
	now := s.clock.Now()
	return businessapi.DocumentUpload{
		Document: toDocument(doc),
		Upload: businessapi.DocumentUploadTarget{
			ExpiresAt: now.Add(uploadTTL).UTC(),
			Headers:   businessapi.DocumentUploadHeaders{ContentType: body.ContentType},
			Method:    businessapi.PUT,
			Url:       s.presign.putURL(key, string(body.ContentType), now),
		},
	}, nil
}
