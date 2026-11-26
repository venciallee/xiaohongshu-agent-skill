package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

type Platform struct {
	Verified string `json:"verified"`
	Note     string `json:"note"`
	Limits   struct {
		TitleMax   int    `json:"title_max"`
		TitleCount string `json:"title_count"`
		BodyMax    int    `json:"body_max"`
		ImagesMax  int    `json:"images_max"`
		ImagePx    []int  `json:"image_px"`
		ImageRatio string `json:"image_ratio"`
	} `json:"limits"`
	AIDisclosure struct {
		Required bool     `json:"required"`
		Rule     string   `json:"rule"`
		Sources  []string `json:"sources"`
	} `json:"ai_disclosure"`
	Automation struct {
		Rule    string   `json:"rule"`
		Sources []string `json:"sources"`
	} `json:"automation"`
	Diversion        []PatternRule `json:"diversion"`
	DiversionSources []string      `json:"diversion_sources"`
	Inducement       []PatternRule `json:"inducement"`
	AbsoluteWords    struct {
		Severity   string              `json:"severity"`
		Note       string              `json:"note"`
		Words      []string            `json:"words"`
		AllowAfter map[string][]string `json:"allow_after"`
	} `json:"absolute_words"`
	HypeWords struct {
		Severity string   `json:"severity"`
		Words    []string `json:"words"`
	} `json:"hype_words"`
	Markdown struct {
		Regex    string `json:"regex"`
		Severity string `json:"severity"`
		Name     string `json:"name"`
	} `json:"markdown"`
}

type PatternRule struct {
	ID       string `json:"id"`
	Regex    string `json:"regex"`
	Name     string `json:"name"`
	Severity string `json:"severity"`
	re       *regexp.Regexp
}

func loadPlatform() *Platform {
	var p Platform
	mustLoad("platform.json", &p)
	compile := func(rs []PatternRule) {
		for i := range rs {
			re, err := regexp.Compile(rs[i].Regex)
			if err != nil {
				fmt.Fprintf(os.Stderr, "platform.json %s 正则无效: %v\n", rs[i].ID, err)
				continue
			}
			rs[i].re = re
		}
	}
	compile(p.Diversion)
	compile(p.Inducement)
	return &p
}

// findAbsolute returns 极限词 hits, skipping allowed continuations such as
// 第一次 / 第一步, which are ordinal, not claims.
func (p *Platform) findAbsolute(s string) []string {
	var hits []string
	for _, w := range p.AbsoluteWords.Words {
		idx := 0
		for {
			i := strings.Index(s[idx:], w)
			if i < 0 {
				break
			}
			pos := idx + i
			after := s[pos+len(w):]
			allowed := false
			for _, a := range p.AbsoluteWords.AllowAfter[w] {
				if strings.HasPrefix(after, a) {
					allowed = true
					break
				}
			}
			if !allowed {
				hits = append(hits, w)
			}
			idx = pos + len(w)
		}
	}
	return uniq(hits)
}

func (p *Platform) findHype(s string) []string {
	var hits []string
	for _, w := range p.HypeWords.Words {
		if strings.Contains(s, w) {
			hits = append(hits, w)
		}
	}
	return hits
}

type PatternHit struct {
	Name     string   `json:"name"`
	Severity string   `json:"severity"`
	Matches  []string `json:"matches"`
}

func matchRules(rs []PatternRule, s string) []PatternHit {
	var out []PatternHit
	for _, r := range rs {
		if r.re == nil {
			continue
		}
		if m := r.re.FindAllString(s, -1); len(m) > 0 {
			for i := range m {
				m[i] = strings.TrimSpace(m[i])
			}
			out = append(out, PatternHit{Name: r.Name, Severity: r.Severity, Matches: uniq(m)})
		}
	}
	return out
}
