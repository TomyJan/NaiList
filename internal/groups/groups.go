package groups

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type Group struct {
	ID           int64     `json:"id"`
	OwnerID      int64     `json:"-"`
	Organization string    `json:"organization"`
	Keywords     []string  `json:"keywords"`
	QQNumber     string    `json:"qqNumber"`
	JoinURL      string    `json:"joinUrl"`
	UpdatedAt    time.Time `json:"updatedAt"`
}
type Input struct {
	Organization string `json:"organization"`
	Keywords     string `json:"keywords"`
	QQNumber     string `json:"qqNumber"`
	JoinURL      string `json:"joinUrl"`
}

var numberPattern = regexp.MustCompile(`^[1-9][0-9]{4,11}$`)
var ErrNotFound = errors.New("群不存在或无权操作")
var ErrDuplicate = errors.New("此群号已存在, 请前往首页搜索查看")

func Normalize(in Input) (Group, error) {
	g := Group{Organization: strings.TrimSpace(in.Organization), QQNumber: strings.TrimSpace(in.QQNumber), JoinURL: strings.TrimSpace(in.JoinURL), Keywords: []string{}}
	if g.Organization == "" || utf8.RuneCountInString(g.Organization) > 100 {
		return g, errors.New("学校/单位必填，最多 100 字")
	}
	if !numberPattern.MatchString(g.QQNumber) {
		return g, errors.New("群号须为 5～12 位数字")
	}
	u, err := url.Parse(g.JoinURL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || strings.IndexFunc(g.JoinURL, unicode.IsControl) >= 0 || len(g.JoinURL) > 2048 {
		return g, errors.New("请填写有效的 http/https 群链接")
	}
	if utf8.RuneCountInString(in.Keywords) > 500 {
		return g, errors.New("关键词最多 500 字")
	}
	seen := map[string]bool{}
	for _, s := range strings.FieldsFunc(in.Keywords, func(r rune) bool { return r == ',' || r == '，' }) {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			g.Keywords = append(g.Keywords, s)
			seen[s] = true
		}
	}
	return g, nil
}
