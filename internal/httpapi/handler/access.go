package handler

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/casbin/casbin/v3"
	"github.com/casbin/casbin/v3/model"
	"github.com/casbin/casbin/v3/persist/string-adapter"
)

//go:embed access_model.conf
var accessModel string

//go:embed access_policy.csv
var accessPolicy string

type RoleReader interface {
	AccessState(context.Context, int64) (string, int64, error)
}

type AccessControl struct {
	roles    RoleReader
	enforcer *casbin.Enforcer
}

func NewAccessControl(roles RoleReader) (*AccessControl, error) {
	m, err := model.NewModelFromString(accessModel)
	if err != nil {
		return nil, err
	}
	e, err := casbin.NewEnforcer(m, stringadapter.NewAdapter(accessPolicy))
	if err != nil {
		return nil, err
	}
	return &AccessControl{roles: roles, enforcer: e}, nil
}

func (a *AccessControl) Require(secret, resource string, next http.Handler) http.Handler {
	return AuthenticateJWT(secret, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, _ := authenticatedUserID(r.Context())
		role, version, err := a.roles.AccessState(r.Context(), id)
		if errors.Is(err, sql.ErrNoRows) {
			writeUnauthorized(w)
			return
		}
		if err != nil {
			setRequestError(w, err)
			writeError(w, http.StatusInternalServerError, "could not verify access; try again later")
			return
		}
		if tokenVersion, ok := r.Context().Value(authenticatedVersionContextKey{}).(int64); !ok || tokenVersion != version {
			writeUnauthorized(w)
			return
		}
		own := resource == "me"
		if resource == "profile" {
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			if len(parts) >= 2 {
				owner, parseErr := strconv.ParseInt(parts[1], 10, 64)
				own = parseErr == nil && owner == id
			}
		}
		allowed, err := a.enforcer.Enforce(role, resource, r.Method, own)
		if err != nil {
			setRequestError(w, err)
			writeError(w, http.StatusInternalServerError, "could not verify access; try again later")
			return
		}
		if !allowed {
			writeError(w, http.StatusForbidden, "access denied")
			return
		}
		next.ServeHTTP(w, r)
	}))
}
