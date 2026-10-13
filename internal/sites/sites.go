package sites

import (
	"errors"
	"net"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

type Site struct {
	ID           int64     `json:"id"`
	OwnerID      int64     `json:"-"`
	Name         string    `json:"name"`
	Domain       string    `json:"domain"`
	Organization string    `json:"organization"`
	UpdatedAt    time.Time `json:"updatedAt"`
}
type Input struct {
	Name         string `json:"name"`
	Domain       string `json:"domain"`
	Organization string `json:"organization"`
}

var ErrNotFound = errors.New("站点不存在或无权操作")
var ErrDuplicate = errors.New("此域名已存在, 请前往首页搜索查看")
var labelPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
var tldPattern = regexp.MustCompile(`^[a-z]{2,63}$`)

func Normalize(in Input) (Site, error) {
	s := Site{Name: strings.TrimSpace(in.Name), Domain: strings.TrimPrefix(strings.ToLower(strings.TrimSpace(in.Domain)), "www."), Organization: strings.TrimSpace(in.Organization)}
	if s.Name == "" || utf8.RuneCountInString(s.Name) > 32 {
		return s, errors.New("站点名称必填，最多 32 字")
	}
	if s.Organization == "" || utf8.RuneCountInString(s.Organization) > 32 {
		return s, errors.New("学校/单位必填，最多 32 字")
	}
	labels := strings.Split(s.Domain, ".")
	valid := len(s.Domain) <= 253 && len(labels) >= 2 && net.ParseIP(s.Domain) == nil && tldPattern.MatchString(labels[len(labels)-1])
	for _, label := range labels {
		if !labelPattern.MatchString(label) {
			valid = false
			break
		}
	}
	if !valid {
		return s, errors.New("请输入有效域名，不含协议、端口或路径")
	}
	return s, nil
}
