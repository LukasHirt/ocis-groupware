package graphapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/owncloud/ocis-groupware/internal/auth"
	"github.com/owncloud/ocis-groupware/internal/ocisgraph"
	"github.com/owncloud/ocis-groupware/internal/principals"
	"github.com/owncloud/ocis-groupware/internal/sharing"
)

// Directory says whether a user or a group exists in the directory behind the
// sign-in, with the token of the user who asks. *ocisgraph.Client is one.
type Directory interface {
	User(ctx context.Context, token, id string) (ocisgraph.Entity, error)
	Group(ctx context.Context, token, id string) (ocisgraph.Entity, error)
}

// Roles of a permission. Graph has more; these are the ones a share can give.
const (
	roleRead  = "read"
	roleWrite = "write"
)

var allowedRoles = []string{roleRead, roleWrite}

func roleAccess(role string) (sharing.Access, bool) {
	switch role {
	case roleRead:
		return sharing.Read, true
	case roleWrite:
		return sharing.ReadWrite, true
	}
	return "", false
}

func accessRole(a sharing.Access) string {
	if a == sharing.ReadWrite {
		return roleWrite
	}
	return roleRead
}

// permissionJSON is a Graph calendarPermission. Grantee is an addition: Graph
// names the grantee by e-mail address, which oCIS users do not always have, so
// the permission also says whether it is for a user or a group and by which
// directory id.
type permissionJSON struct {
	ID                   string       `json:"id"`
	EmailAddress         emailAddress `json:"emailAddress"`
	IsInsideOrganization bool         `json:"isInsideOrganization"`
	IsRemovable          bool         `json:"isRemovable"`
	Role                 string       `json:"role"`
	AllowedRoles         []string     `json:"allowedRoles"`
	Grantee              granteeJSON  `json:"grantee"`
}

type granteeJSON struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

func toPermission(m sharing.Member) permissionJSON {
	return permissionJSON{
		ID:                   permissionID(m.Type, m.ID),
		EmailAddress:         emailAddress{Name: m.Name},
		IsInsideOrganization: true,
		IsRemovable:          true,
		Role:                 accessRole(m.Access),
		AllowedRoles:         allowedRoles,
		Grantee:              granteeJSON{Type: string(m.Type), ID: m.ID},
	}
}

// permissionID is the id of the permission that gives a user or group access:
// the unpadded base64url form of "<type>:<id>".
func permissionID(t sharing.GranteeType, id string) string { return encodePart(string(t) + ":" + id) }

func parsePermissionID(s string) (sharing.GranteeType, string, error) {
	text, err := decodePart(s)
	if err != nil {
		return "", "", err
	}
	typ, id, ok := strings.Cut(text, ":")
	if !ok || id == "" || (sharing.GranteeType(typ) != sharing.User && sharing.GranteeType(typ) != sharing.Group) {
		return "", "", errBadID
	}
	return sharing.GranteeType(typ), id, nil
}

// permissionRoutes serves the permissions of one kind of collection.
type permissionRoutes struct {
	a *Handler
	// find resolves the id in the URL to a collection the user can reach.
	find func(ctx context.Context, user principals.Principal, id string) (CalendarEntry, *apiError)
	noun string
}

func (a *Handler) calendarPermissionRoutes() []route {
	pr := permissionRoutes{a: a, noun: "calendar",
		find: func(ctx context.Context, user principals.Principal, id string) (CalendarEntry, *apiError) {
			e, _, aerr := a.findCalendar(ctx, user, id)
			return e, aerr
		}}
	return pr.routes("me/calendars/{id}/calendarPermissions")
}

func (pr permissionRoutes) routes(base string) []route {
	split := func(s string) []string { return strings.Split(s, "/") }
	return []route{
		{http.MethodGet, split(base), pr.list},
		{http.MethodPost, split(base), pr.create},
		{http.MethodGet, split(base + "/{pid}"), pr.get},
		{http.MethodPatch, split(base + "/{pid}"), pr.change},
		{http.MethodDelete, split(base + "/{pid}"), pr.remove},
	}
}

// owned resolves the collection of the request and checks the caller owns it.
// A collection the caller cannot reach is 404, one they can reach but not
// manage is 403.
func (pr permissionRoutes) owned(r *http.Request, c *call) (CalendarEntry, *apiError) {
	e, aerr := pr.find(r.Context(), c.caller, c.params["id"])
	if aerr != nil {
		return CalendarEntry{}, aerr
	}
	if e.Collection.Ref.Owner != c.caller.Owner || !e.Collection.Access.AllowsPropertyWrites() {
		return CalendarEntry{}, forbidden("only the owner can manage who a " + pr.noun + " is shared with")
	}
	return e, nil
}

func (pr permissionRoutes) list(w http.ResponseWriter, r *http.Request, c *call) {
	e, aerr := pr.owned(r, c)
	if aerr != nil {
		writeError(w, aerr)
		return
	}
	members := pr.a.cfg.Shares.Members(e.Collection.Ref)
	out := make([]permissionJSON, len(members))
	for i, m := range members {
		out[i] = toPermission(m)
	}
	writeCollection(w, r, out)
}

// member returns the member the permission id in the URL names.
func (pr permissionRoutes) member(e CalendarEntry, c *call) (sharing.Member, *apiError) {
	t, id, err := parsePermissionID(c.params["pid"])
	if err != nil {
		return sharing.Member{}, notFound("the permission does not exist")
	}
	for _, m := range pr.a.cfg.Shares.Members(e.Collection.Ref) {
		if m.Type == t && m.ID == id {
			return m, nil
		}
	}
	return sharing.Member{}, notFound("the permission does not exist")
}

func (pr permissionRoutes) get(w http.ResponseWriter, r *http.Request, c *call) {
	e, aerr := pr.owned(r, c)
	if aerr != nil {
		writeError(w, aerr)
		return
	}
	m, aerr := pr.member(e, c)
	if aerr != nil {
		writeError(w, aerr)
		return
	}
	writeJSON(w, http.StatusOK, toPermission(m))
}

// permissionChange is the body of a POST or PATCH. A PATCH takes the role
// only; the grantee of a permission does not change.
type permissionChange struct {
	Role    *string      `json:"role"`
	Grantee *granteeJSON `json:"grantee"`
}

func parseRole(role *string) (sharing.Access, *apiError) {
	if role == nil {
		return "", badRequest("role is required")
	}
	acc, ok := roleAccess(*role)
	if !ok {
		return "", badRequest("role must be read or write")
	}
	return acc, nil
}

func (pr permissionRoutes) create(w http.ResponseWriter, r *http.Request, c *call) {
	e, aerr := pr.owned(r, c)
	if aerr != nil {
		writeError(w, aerr)
		return
	}
	var ch permissionChange
	if aerr := decodeJSON(w, r, &ch); aerr != nil {
		writeError(w, aerr)
		return
	}
	acc, aerr := parseRole(ch.Role)
	if aerr != nil {
		writeError(w, aerr)
		return
	}
	if ch.Grantee == nil || ch.Grantee.ID == "" {
		writeError(w, badRequest("grantee needs a type and an id"))
		return
	}
	typ := sharing.GranteeType(ch.Grantee.Type)
	if typ != sharing.User && typ != sharing.Group {
		writeError(w, badRequest("grantee type must be user or group"))
		return
	}
	if existing := pr.a.cfg.Shares.Members(e.Collection.Ref); containsMember(existing, typ, ch.Grantee.ID) {
		writeError(w, newError(http.StatusConflict, codeNameExists, "the "+pr.noun+" is already shared with this "+string(typ)))
		return
	}
	name, aerr := pr.a.checkGrantee(r, c, typ, ch.Grantee.ID)
	if aerr != nil {
		writeError(w, aerr)
		return
	}
	m := sharing.Member{Type: typ, ID: ch.Grantee.ID, Name: name, Access: acc}
	if err := pr.a.cfg.Shares.Grant(r.Context(), e.Collection.Ref, e.Collection.ID, m); err != nil {
		writeError(w, pr.a.shareError("share a "+pr.noun, c.caller, err))
		return
	}
	writeJSON(w, http.StatusCreated, toPermission(m))
}

func containsMember(ms []sharing.Member, t sharing.GranteeType, id string) bool {
	for _, m := range ms {
		if m.Type == t && m.ID == id {
			return true
		}
	}
	return false
}

// checkGrantee asks the directory, with the caller's token, whether the user
// or group exists, and returns its display name.
func (a *Handler) checkGrantee(r *http.Request, c *call, t sharing.GranteeType, id string) (string, *apiError) {
	token, _ := auth.TokenFromContext(r.Context())
	var (
		ent ocisgraph.Entity
		err error
	)
	if t == sharing.User {
		ent, err = a.cfg.Directory.User(r.Context(), token, id)
	} else {
		ent, err = a.cfg.Directory.Group(r.Context(), token, id)
	}
	switch {
	case err == nil:
		return ent.DisplayName, nil
	case errors.Is(err, ocisgraph.ErrNotFound):
		return "", badRequest("the " + string(t) + " does not exist")
	}
	a.logger.Error("could not check a grantee in the directory", "owner", c.caller.Owner, "type", string(t), "error", err)
	return "", newError(http.StatusBadGateway, codeGeneral, "could not check the "+string(t)+" in the directory")
}

func (pr permissionRoutes) change(w http.ResponseWriter, r *http.Request, c *call) {
	e, aerr := pr.owned(r, c)
	if aerr != nil {
		writeError(w, aerr)
		return
	}
	m, aerr := pr.member(e, c)
	if aerr != nil {
		writeError(w, aerr)
		return
	}
	var ch permissionChange
	if aerr := decodeJSON(w, r, &ch); aerr != nil {
		writeError(w, aerr)
		return
	}
	acc, aerr := parseRole(ch.Role)
	if aerr != nil {
		writeError(w, aerr)
		return
	}
	m.Access = acc
	if err := pr.a.cfg.Shares.Grant(r.Context(), e.Collection.Ref, e.Collection.ID, m); err != nil {
		writeError(w, pr.a.shareError("change a share", c.caller, err))
		return
	}
	writeJSON(w, http.StatusOK, toPermission(m))
}

func (pr permissionRoutes) remove(w http.ResponseWriter, r *http.Request, c *call) {
	e, aerr := pr.owned(r, c)
	if aerr != nil {
		writeError(w, aerr)
		return
	}
	t, id, err := parsePermissionID(c.params["pid"])
	if err != nil {
		writeError(w, notFound("the permission does not exist"))
		return
	}
	if err := pr.a.cfg.Shares.Revoke(r.Context(), e.Collection.Ref, t, id); err != nil {
		writeError(w, pr.a.shareError("revoke a share", c.caller, err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// shareError maps an error of the sharing package to the answer.
func (a *Handler) shareError(action string, user principals.Principal, err error) *apiError {
	switch {
	case errors.Is(err, sharing.ErrNotShared):
		return notFound("the permission does not exist")
	case errors.Is(err, sharing.ErrSelfShare):
		return badRequest("the owner already has access")
	case errors.Is(err, sharing.ErrTooManyMembers):
		return newError(http.StatusConflict, codeInvalidRequest, "the item is shared with as many users and groups as it can be")
	case errors.Is(err, sharing.ErrInvalidMember):
		return badRequest("the grantee is not valid")
	}
	a.logger.Error("could not "+action, "owner", user.Owner, "error", err)
	return internalError()
}
