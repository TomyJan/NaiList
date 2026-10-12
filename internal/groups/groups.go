package groups

import (
	"errors"
	"regexp"
	"strings"
	"time"
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

var numberPattern = regexp.MustCompile(`^[0-9]{5,11}$`)
var joinPattern = regexp.MustCompile(`^https://(?:qm\.qq\.com/q/[A-Za-z0-9_-]+|qun\.qq\.com/universal-share/share\?(?:[A-Za-z0-9._~!$&*+,;=:@/?-]|%[A-Fa-f0-9]{2})+)$`)
var ErrNotFound = errors.New("群不存在或无权操作")
var ErrDuplicate = errors.New("此群号已存在, 请前往首页搜索查看")

func Normalize(in Input) (Group, error) {
	g := Group{Organization: strings.TrimSpace(in.Organization), QQNumber: strings.TrimSpace(in.QQNumber), JoinURL: strings.TrimSpace(in.JoinURL), Keywords: []string{}}
	if g.Organization == "" || utf8.RuneCountInString(g.Organization) > 32 {
		return g, errors.New("学校/单位必填，最多 32 字")
	}
	if !numberPattern.MatchString(g.QQNumber) {
		return g, errors.New("群号须为 5～11 位数字")
	}
	if !joinPattern.MatchString(g.JoinURL) || len(g.JoinURL) > 2048 {
		return g, errors.New("请输入有效 QQ 群链接（支持新版短链接与旧版分享链接）")
	}
	if utf8.RuneCountInString(in.Keywords) > 128 {
		return g, errors.New("关键词最多 128 字")
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
