package server

import (
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	"io"
	"nailist/internal/admin"
	"nailist/internal/groups"
	"nailist/internal/logging"
	"net/http"
	"time"
)

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := r.Context().Value(userKey).(User)
		if !s.cfg.AdminUserIDs[u.ID] {
			rejectRequest(w, r, 403, "admin.access.rejected", "administrator_required", "无管理权限")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	requestLogger(r).DebugContext(r.Context(), "开始读取全站账号、有效奶群及站点计数与公开登录方式", "component", "admin", "event", "admin.users.list.started")
	users, err := (admin.Store{DB: s.db}).Users(r.Context())
	if err != nil {
		failure(w, r, err)
		return
	}
	requestLogger(r).DebugContext(r.Context(), "全站账号及登录方式读取完成", "component", "admin", "event", "admin.users.list.completed", "count", len(users), "duration_ms", logging.DurationMS(started))
	respond(w, r, 200, users)
}
func (s *Server) adminGroups(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	requestLogger(r).DebugContext(r.Context(), "开始读取全站有效奶群及所属账号", "component", "admin", "event", "admin.groups.list.started")
	groups, err := (admin.Store{DB: s.db}).Groups(r.Context())
	if err != nil {
		failure(w, r, err)
		return
	}
	requestLogger(r).DebugContext(r.Context(), "全站有效奶群及归属读取完成", "component", "admin", "event", "admin.groups.list.completed", "count", len(groups), "duration_ms", logging.DurationMS(started))
	respond(w, r, 200, groups)
}
func adminID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := groupID(r)
	if err != nil || id < 1 {
		rejectRequest(w, r, 400, "admin.write.rejected", "invalid_id", "无效 ID")
		return 0, false
	}
	return id, true
}
func adminError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, admin.ErrNotFound):
		rejectRequest(w, r, 404, "admin.write.rejected", "not_found", err.Error())
	case errors.Is(err, admin.ErrLastIdentity):
		rejectRequest(w, r, 409, "admin.write.rejected", "last_identity", err.Error())
	case errors.Is(err, admin.ErrOwnAccount):
		rejectRequest(w, r, 409, "admin.write.rejected", "self_delete", err.Error())
	case errors.Is(err, admin.ErrActiveSites):
		rejectRequest(w, r, 409, "admin.write.rejected", "active_sites", err.Error())
	case errors.Is(err, admin.ErrActiveGroups):
		rejectRequest(w, r, 409, "admin.write.rejected", "active_groups", err.Error())
	default:
		failure(w, r, err)
	}
}
func (s *Server) adminUnbind(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	id, ok := adminID(w, r)
	if !ok {
		return
	}
	provider := chi.URLParam(r, "provider")
	requestDetails(r, "account_id", id, "provider", provider)
	revoked, err := (admin.Store{DB: s.db}).Unbind(r.Context(), id, provider)
	if err != nil {
		adminError(w, r, err)
		return
	}
	requestLogger(r).InfoContext(r.Context(), "管理员已解绑登录方式并撤销账号会话", "component", "admin", "event", "admin.identity.unbound", "outcome", "success", "admin_id", r.Context().Value(userKey).(User).ID, "account_id", id, "provider", provider, "revoked_sessions", revoked.RevokedSessions, "deleted_flows", revoked.DeletedFlows, "deleted_identities", revoked.DeletedIdentities, "duration_ms", logging.DurationMS(started))
	w.WriteHeader(204)
}
func (s *Server) adminDeleteUser(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	id, ok := adminID(w, r)
	if !ok {
		return
	}
	actor := r.Context().Value(userKey).(User).ID
	requestDetails(r, "account_id", id)
	revoked, err := (admin.Store{DB: s.db}).DeleteUser(r.Context(), actor, id)
	if err != nil {
		adminError(w, r, err)
		return
	}
	s.groupsCache.invalidate(r.Context(), "groups")
	s.sitesCache.invalidate(r.Context(), "sites")
	requestLogger(r).InfoContext(r.Context(), "管理员已删除账号及历史资源", "component", "admin", "event", "admin.user.deleted", "outcome", "success", "admin_id", actor, "account_id", id, "revoked_sessions", revoked.RevokedSessions, "deleted_flows", revoked.DeletedFlows, "deleted_identities", revoked.DeletedIdentities, "deleted_groups", revoked.DeletedGroups, "deleted_sites", revoked.DeletedSites, "duration_ms", logging.DurationMS(started))
	w.WriteHeader(204)
}
func (s *Server) adminSaveGroup(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	id, ok := adminID(w, r)
	if !ok {
		return
	}
	var in struct {
		groups.Input
		OwnerID int64 `json:"ownerId"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		rejectRequest(w, r, 400, "admin.group.rejected", "invalid_json", "表单格式错误")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		rejectRequest(w, r, 400, "admin.group.rejected", "trailing_json", "表单格式错误")
		return
	}
	if in.OwnerID < 1 {
		rejectRequest(w, r, 400, "admin.group.rejected", "invalid_owner", "请选择有效所属账号")
		return
	}
	g, err := groups.Normalize(in.Input)
	if err != nil {
		rejectRequest(w, r, 400, "admin.group.rejected", "validation_failed", err.Error())
		return
	}
	requestDetails(r, "group_id", id, "new_owner_id", in.OwnerID)
	requestLogger(r).DebugContext(r.Context(), "管理奶群表单通过校验，准备原子保存信息与归属", "component", "admin", "event", "admin.group.save.started", "group_id", id, "new_owner_id", in.OwnerID, "organization", g.Organization, "qq_number", g.QQNumber, "keyword_count", len(g.Keywords), "link_format", joinLinkFormat(g.JoinURL))
	saved, old, err := (admin.Store{DB: s.db}).SaveGroup(r.Context(), id, in.OwnerID, g)
	if err != nil {
		adminError(w, r, err)
		return
	}
	s.groupsCache.invalidate(r.Context(), "groups")
	requestLogger(r).InfoContext(r.Context(), "管理员已保存奶群信息及归属", "component", "admin", "event", "admin.group.saved", "outcome", "success", "admin_id", r.Context().Value(userKey).(User).ID, "group_id", id, "old_owner_id", old, "new_owner_id", in.OwnerID, "organization", saved.Organization, "qq_number", saved.QQNumber, "keyword_count", len(saved.Keywords), "link_format", joinLinkFormat(saved.JoinURL), "duration_ms", logging.DurationMS(started))
	respond(w, r, 200, saved)
}

func (s *Server) adminStats(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	requestLogger(r).DebugContext(r.Context(), "开始聚合站点概览及近 30 天新增趋势", "component", "admin", "event", "admin.stats.started", "timezone", "Asia/Shanghai", "days", 30, "scope", "current_users_active_groups_sites")
	stats, err := (admin.Store{DB: s.db}).Stats(r.Context())
	if err != nil {
		failure(w, r, err)
		return
	}
	requestLogger(r).DebugContext(r.Context(), "站点统计聚合完成，日期缺口已补零", "component", "admin", "event", "admin.stats.completed", "users", stats.Users, "groups", stats.Groups, "sites", stats.Sites, "contributors", stats.Contributors, "organizations", stats.Organizations, "days", len(stats.Daily), "duration_ms", logging.DurationMS(started))
	respond(w, r, 200, stats)
}
