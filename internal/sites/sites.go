package sites

import (
	"errors"
	"net"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/idna"
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

// Normalize validates IDNA before accepting an internationalized TLD's A-label.
var tldPattern = regexp.MustCompile(`^(?:[a-z]{2,63}|xn--[a-z0-9-]{1,59})$`)

func Normalize(in Input) (Site, error) {
	s := Site{Name: strings.TrimSpace(in.Name), Domain: strings.TrimPrefix(strings.ToLower(strings.TrimSpace(in.Domain)), "www."), Organization: strings.TrimSpace(in.Organization)}
	if s.Name == "" || utf8.RuneCountInString(s.Name) > 32 {
		return s, errors.New("站点名称必填，最多 32 字")
	}
	if s.Organization == "" || utf8.RuneCountInString(s.Organization) > 32 {
		return s, errors.New("学校/单位必填，最多 32 字")
	}
	domain, err := idna.Lookup.ToASCII(s.Domain)
	if err != nil {
		return s, errors.New("请输入有效域名，不含协议、端口或路径")
	}
	s.Domain = strings.TrimPrefix(strings.ToLower(domain), "www.")
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
