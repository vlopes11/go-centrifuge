package pending

import (
	"bytes"
	"context"

	coredocumentpb "github.com/centrifuge/centrifuge-protobufs/gen/go/coredocument"
	"github.com/centrifuge/go-centrifuge/contextutil"
	"github.com/centrifuge/go-centrifuge/documents"
	"github.com/centrifuge/go-centrifuge/errors"
	"github.com/centrifuge/go-centrifuge/identity"
	"github.com/centrifuge/go-centrifuge/jobs"
	"github.com/centrifuge/go-centrifuge/utils/byteutils"
)

// ErrPendingDocumentExists is a sentinel error used when document was created and tried to create a new one.
const ErrPendingDocumentExists = errors.Error("Pending document already created")

// Service provides an interface for functions common to all document types
type Service interface {
	// Get returns the document associated with docID and Status.
	Get(ctx context.Context, docID []byte, status documents.Status) (documents.Model, error)

	// GetVersion returns the document associated with docID and versionID.
	GetVersion(ctx context.Context, docID, versionID []byte) (documents.Model, error)

	// Update updates a pending document from the payload
	Update(ctx context.Context, payload documents.UpdatePayload) (documents.Model, error)

	// Create creates a pending document from the payload
	Create(ctx context.Context, payload documents.UpdatePayload) (documents.Model, error)

	// Commit validates, shares and anchors document
	Commit(ctx context.Context, docID []byte) (documents.Model, jobs.JobID, error)

	// AddSignedAttribute signs the value using the account keys and adds the attribute to the pending document.
	AddSignedAttribute(ctx context.Context, docID []byte, label string, value []byte) (documents.Model, error)

	// RemoveCollaborators removes collaborators from the document.
	RemoveCollaborators(ctx context.Context, docID []byte, dids []identity.DID) (documents.Model, error)

	// GetRole returns specific role in the latest version of the document.
	GetRole(ctx context.Context, docID, roleID []byte) (*coredocumentpb.Role, error)

	// AddRole adds a new role to given document
	AddRole(ctx context.Context, docID []byte, roleKey string, collabs []identity.DID) (*coredocumentpb.Role, error)

	// UpdateRole updates a role in the given document
	UpdateRole(ctx context.Context, docID, roleID []byte, collabs []identity.DID) (*coredocumentpb.Role, error)

	// AddTransitionRules creates transition rules to the given document.
	// The access is only given to the roleKey which is expected to be present already.
	AddTransitionRules(ctx context.Context, docID []byte, addRules AddTransitionRules) ([]*coredocumentpb.TransitionRule, error)

	// GetTransitionRule returns the transition rule associated with ruleID from the latest version of the document.
	GetTransitionRule(ctx context.Context, docID, ruleID []byte) (*coredocumentpb.TransitionRule, error)

	// DeleteTransitionRule deletes the transition rule associated with ruleID in th document.
	DeleteTransitionRule(ctx context.Context, docID, ruleID []byte) error
}

// service implements Service
type service struct {
	docSrv      documents.Service
	pendingRepo Repository
}

// DefaultService returns the default implementation of the service
func DefaultService(docSrv documents.Service, repo Repository) Service {
	return service{
		docSrv:      docSrv,
		pendingRepo: repo,
	}
}

func (s service) getDocumentAndAccount(ctx context.Context, docID []byte) (doc documents.Model, did identity.DID, err error) {
	did, err = contextutil.AccountDID(ctx)
	if err != nil {
		return doc, did, contextutil.ErrDIDMissingFromContext
	}

	doc, err = s.pendingRepo.Get(did[:], docID)
	if err != nil {
		return doc, did, documents.ErrDocumentNotFound
	}

	return doc, did, nil
}

// Get returns the document associated with docID
// If status is pending, we return the pending document from pending repo.
// else, we defer Get to document service.
func (s service) Get(ctx context.Context, docID []byte, status documents.Status) (documents.Model, error) {
	if status != documents.Pending {
		return s.docSrv.GetCurrentVersion(ctx, docID)
	}

	did, err := contextutil.AccountDID(ctx)
	if err != nil {
		return nil, contextutil.ErrDIDMissingFromContext
	}

	return s.pendingRepo.Get(did[:], docID)
}

// GetVersion return the specific version of the document
// We try to fetch the version from the document service, if found return
// else look in pending repo for specific version.
func (s service) GetVersion(ctx context.Context, docID, versionID []byte) (documents.Model, error) {
	doc, err := s.docSrv.GetVersion(ctx, docID, versionID)
	if err == nil {
		return doc, nil
	}

	accID, err := contextutil.AccountDID(ctx)
	if err != nil {
		return nil, contextutil.ErrDIDMissingFromContext
	}

	doc, err = s.pendingRepo.Get(accID[:], docID)
	if err != nil || !bytes.Equal(versionID, doc.CurrentVersion()) {
		return nil, documents.ErrDocumentNotFound
	}

	return doc, nil
}

// Create creates either a new document or next version of an anchored document and stores the document.
// errors out if there an pending document created already
func (s service) Create(ctx context.Context, payload documents.UpdatePayload) (documents.Model, error) {
	accID, err := contextutil.AccountDID(ctx)
	if err != nil {
		return nil, contextutil.ErrDIDMissingFromContext
	}

	if len(payload.DocumentID) > 0 {
		_, err := s.pendingRepo.Get(accID[:], payload.DocumentID)
		if err == nil {
			// found an existing pending document. error out
			return nil, ErrPendingDocumentExists
		}
	}

	doc, err := s.docSrv.Derive(ctx, payload)
	if err != nil {
		return nil, err
	}

	// we create one document per ID. hence, we use ID instead of current version
	// since its common to all document versions.
	return doc, s.pendingRepo.Create(accID[:], doc.ID(), doc)
}

// Update updates a pending document from the payload
func (s service) Update(ctx context.Context, payload documents.UpdatePayload) (documents.Model, error) {
	m, accID, err := s.getDocumentAndAccount(ctx, payload.DocumentID)
	if err != nil {
		return nil, err
	}

	mp, ok := m.(documents.Patcher)
	if !ok {
		return nil, documents.ErrNotPatcher
	}

	err = mp.Patch(payload)
	if err != nil {
		return nil, err
	}
	doc := mp.(documents.Model)
	return doc, s.pendingRepo.Update(accID[:], doc.ID(), doc)
}

// Commit triggers validations, state change and anchor job
func (s service) Commit(ctx context.Context, docID []byte) (documents.Model, jobs.JobID, error) {
	doc, accID, err := s.getDocumentAndAccount(ctx, docID)
	if err != nil {
		return nil, jobs.NilJobID(), err
	}

	jobID, err := s.docSrv.Commit(ctx, doc)
	if err != nil {
		return nil, jobs.NilJobID(), err
	}

	return doc, jobID, s.pendingRepo.Delete(accID[:], docID)
}

func (s service) AddSignedAttribute(ctx context.Context, docID []byte, label string, value []byte) (documents.Model, error) {
	acc, err := contextutil.Account(ctx)
	if err != nil {
		return nil, contextutil.ErrDIDMissingFromContext
	}

	model, err := s.pendingRepo.Get(acc.GetIdentityID(), docID)
	if err != nil {
		return nil, documents.ErrDocumentNotFound
	}

	did, err := identity.NewDIDFromBytes(acc.GetIdentityID())
	if err != nil {
		return nil, err
	}

	// we use currentVersion here since the version is not anchored yet
	attr, err := documents.NewSignedAttribute(label, did, acc, model.ID(), model.CurrentVersion(), value)
	if err != nil {
		return nil, err
	}

	err = model.AddAttributes(documents.CollaboratorsAccess{}, false, attr)
	if err != nil {
		return nil, err
	}

	return model, s.pendingRepo.Update(acc.GetIdentityID(), docID, model)
}

// RemoveCollaborators removes dids from the given document.
func (s service) RemoveCollaborators(ctx context.Context, docID []byte, dids []identity.DID) (documents.Model, error) {
	doc, accID, err := s.getDocumentAndAccount(ctx, docID)
	if err != nil {
		return nil, err
	}

	err = doc.RemoveCollaborators(dids)
	if err != nil {
		return nil, err
	}

	return doc, s.pendingRepo.Update(accID[:], docID, doc)
}

func (s service) GetRole(ctx context.Context, docID, roleID []byte) (*coredocumentpb.Role, error) {
	doc, _, err := s.getDocumentAndAccount(ctx, docID)
	if err == nil {
		return doc.GetRole(roleID)
	}

	if err == contextutil.ErrDIDMissingFromContext {
		return nil, err
	}

	// fetch the document from the doc service
	doc, err = s.docSrv.GetCurrentVersion(ctx, docID)
	if err != nil {
		return nil, documents.ErrDocumentNotFound
	}

	return doc.GetRole(roleID)
}

// AddRole adds a new role to given document
func (s service) AddRole(ctx context.Context, docID []byte, roleKey string, collabs []identity.DID) (*coredocumentpb.Role, error) {
	doc, accID, err := s.getDocumentAndAccount(ctx, docID)
	if err != nil {
		return nil, err
	}

	r, err := doc.AddRole(roleKey, collabs)
	if err != nil {
		return nil, err
	}

	return r, s.pendingRepo.Update(accID[:], docID, doc)
}

// UpdateRole updates a role in the given document
func (s service) UpdateRole(ctx context.Context, docID, roleID []byte, collabs []identity.DID) (*coredocumentpb.Role, error) {
	doc, accID, err := s.getDocumentAndAccount(ctx, docID)
	if err != nil {
		return nil, err
	}

	r, err := doc.UpdateRole(roleID, collabs)
	if err != nil {
		return nil, err
	}

	return r, s.pendingRepo.Update(accID[:], docID, doc)
}

// AttributeRule contains Attribute key label for which the rule has to be created
// with write access enabled to RoleID
// Note: role ID should already exist in the document.
type AttributeRule struct {
	// attribute key label
	KeyLabel string `json:"key_label"`

	// roleID is 32 byte role ID in hex. RoleID should already be part of the document.
	RoleID byteutils.HexBytes `json:"role_id" swaggertype:"primitive,string"`
}

// AddTransitionRules contains list of attribute rules to be created.
type AddTransitionRules struct {
	AttributeRules []AttributeRule `json:"attribute_rules"`
}

func (s service) AddTransitionRules(ctx context.Context, docID []byte, addRules AddTransitionRules) ([]*coredocumentpb.TransitionRule, error) {
	doc, accID, err := s.getDocumentAndAccount(ctx, docID)
	if err != nil {
		return nil, err
	}

	var rules []*coredocumentpb.TransitionRule
	for _, r := range addRules.AttributeRules {
		key, err := documents.AttrKeyFromLabel(r.KeyLabel)
		if err != nil {
			return nil, err
		}

		rule, err := doc.AddTransitionRuleForAttribute(r.RoleID[:], key)
		if err != nil {
			return nil, err
		}

		rules = append(rules, rule)
	}

	return rules, s.pendingRepo.Update(accID[:], docID, doc)
}

func (s service) GetTransitionRule(ctx context.Context, docID, ruleID []byte) (*coredocumentpb.TransitionRule, error) {
	doc, _, err := s.getDocumentAndAccount(ctx, docID)
	if err == nil {
		return doc.GetTransitionRule(ruleID)
	}

	if err == contextutil.ErrDIDMissingFromContext {
		return nil, err
	}

	// fetch the document from the doc service
	doc, err = s.docSrv.GetCurrentVersion(ctx, docID)
	if err != nil {
		return nil, documents.ErrDocumentNotFound
	}

	return doc.GetTransitionRule(ruleID)
}

func (s service) DeleteTransitionRule(ctx context.Context, docID, ruleID []byte) error {
	doc, did, err := s.getDocumentAndAccount(ctx, docID)
	if err != nil {
		return err
	}

	err = doc.DeleteTransitionRule(ruleID)
	if err != nil {
		return err
	}

	return s.pendingRepo.Update(did[:], docID, doc)
}
