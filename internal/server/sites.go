package server

import (
	"context"
	"encoding/json"
	"io"
	"nailist/internal/admin"
	"nailist/internal/logging"
	"nailist/internal/sites"
	"net/http"
	"time"
)

type SiteStore interface {
	List(context.Context, int64) ([]sites.Site, error)
	Save(context.Context, int64, int64, sites.Site) (sites.Site, error)
	Delete(context.Context, int64, int64) error
}

func (s *Server) listSites(w http.ResponseWriter, r *http.Request) { s.readSites(w, r, 0) }
func (s *Server) mySites(w http.ResponseWriter, r *http.Request) {
	s.readSites(w, r, r.Context().Value(userKey).(User).ID)
}
func (s *Server) readSites(w http.ResponseWriter, r *http.Request, owner int64) {
	started := time.Now()
	scope := "public"
	if owner != 0 {
		scope = "owner"
	}
	requestLogger(r).DebugContext(r.Context(), "开始读取有效站点列表", "component", "sites", "event", "sites.list.started", "scope", scope)
	rows, err := s.siteStore.List(r.Context(), owner)
	if err != nil {
		failure(w, r, err)
		return
	}
	requestDetails(r, "count", len(rows), "scope", scope)
	requestLogger(r).DebugContext(r.Context(), "有效站点列表读取完成", "component", "sites", "event", "sites.list.completed", "scope", scope, "count", len(rows), "duration_ms", logging.DurationMS(started))
	respond(w, r, 200, rows)
}
func siteID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := groupID(r)
	if err != nil || id < 1 {
		rejectRequest(w, r, 400, "sites.write.rejected", "invalid_site_id", "无效站点 ID")
		return 0, false
	}
	return id, true
}
func decodeSite(w http.ResponseWriter, r *http.Request, in any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(in); err != nil {
		rejectRequest(w, r, 400, "sites.save.rejected", "invalid_json", "表单格式错误", "body_limit_bytes", 16384)
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		rejectRequest(w, r, 400, "sites.save.rejected", "trailing_json", "表单格式错误")
		return false
	}
	return true
}
func (s *Server) saveSite(w http.ResponseWriter, r *http.Request) {
	var id int64
	if r.Method == "PUT" {
		var ok bool
		id, ok = siteID(w, r)
		if !ok {
			return
		}
	}
	var in sites.Input
	if !decodeSite(w, r, &in) {
		return
	}
	site, err := sites.Normalize(in)
	if err != nil {
		rejectRequest(w, r, 400, "sites.save.rejected", "validation_failed", err.Error(), "site_id", id)
		return
	}
	owner := r.Context().Value(userKey).(User).ID
	started := time.Now()
	requestLogger(r).DebugContext(r.Context(), "站点表单通过校验，准备保存", "component", "sites", "event", "sites.save.started", "site_id", id, "domain", site.Domain, "name", site.Name, "organization", site.Organization)
	site, err = s.siteStore.Save(r.Context(), owner, id, site)
	if err != nil {
		failOperation(w, r, err, "sites.save.failed", "store_failed", "site_id", id, "domain", site.Domain)
		return
	}
	status := 200
	if id == 0 {
		status = 201
	}
	requestDetails(r, "site_id", site.ID)
	requestLogger(r).InfoContext(r.Context(), "站点已保存", "component", "sites", "event", "sites.save.completed", "outcome", "success", "site_id", site.ID, "domain", site.Domain, "name", site.Name, "organization", site.Organization, "duration_ms", logging.DurationMS(started))
	respond(w, r, status, site)
}
func (s *Server) removeSite(w http.ResponseWriter, r *http.Request) {
	id, ok := siteID(w, r)
	if !ok {
		return
	}
	owner := r.Context().Value(userKey).(User).ID
	started := time.Now()
	requestLogger(r).DebugContext(r.Context(), "按站点 ID 和所属账号执行软删除", "component", "sites", "event", "sites.delete.started", "site_id", id)
	if err := s.siteStore.Delete(r.Context(), owner, id); err != nil {
		failOperation(w, r, err, "sites.delete.failed", "store_failed", "site_id", id)
		return
	}
	requestDetails(r, "site_id", id)
	requestLogger(r).InfoContext(r.Context(), "站点已软删除", "component", "sites", "event", "sites.delete.completed", "outcome", "success", "site_id", id, "duration_ms", logging.DurationMS(started))
	w.WriteHeader(204)
}
func (s *Server) adminSites(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	requestLogger(r).DebugContext(r.Context(), "开始读取全站有效站点及所属账号", "component", "admin", "event", "admin.sites.list.started")
	rows, err := (admin.Store{DB: s.db}).Sites(r.Context())
	if err != nil {
		failure(w, r, err)
		return
	}
	requestLogger(r).DebugContext(r.Context(), "全站站点及归属读取完成", "component", "admin", "event", "admin.sites.list.completed", "count", len(rows), "duration_ms", logging.DurationMS(started))
	respond(w, r, 200, rows)
}
func (s *Server) adminSaveSite(w http.ResponseWriter, r *http.Request) {
	id, ok := siteID(w, r)
	if !ok {
		return
	}
	var in struct {
		sites.Input
		OwnerID int64 `json:"ownerId"`
	}
	if !decodeSite(w, r, &in) {
		return
	}
	if in.OwnerID < 1 {
		rejectRequest(w, r, 400, "admin.site.rejected", "invalid_owner", "请选择有效所属账号")
		return
	}
	site, err := sites.Normalize(in.Input)
	if err != nil {
		rejectRequest(w, r, 400, "admin.site.rejected", "validation_failed", err.Error())
		return
	}
	started := time.Now()
	requestDetails(r, "site_id", id, "new_owner_id", in.OwnerID)
	requestLogger(r).DebugContext(r.Context(), "管理站点表单通过校验，准备原子保存信息与归属", "component", "admin", "event", "admin.site.save.started", "site_id", id, "new_owner_id", in.OwnerID, "domain", site.Domain, "name", site.Name, "organization", site.Organization)
	saved, old, err := (admin.Store{DB: s.db}).SaveSite(r.Context(), id, in.OwnerID, site)
	if err != nil {
		adminError(w, r, err)
		return
	}
	requestLogger(r).InfoContext(r.Context(), "管理员已保存站点信息及归属", "component", "admin", "event", "admin.site.saved", "outcome", "success", "admin_id", r.Context().Value(userKey).(User).ID, "site_id", id, "old_owner_id", old, "new_owner_id", in.OwnerID, "domain", saved.Domain, "name", saved.Name, "organization", saved.Organization, "duration_ms", logging.DurationMS(started))
	respond(w, r, 200, saved)
}
