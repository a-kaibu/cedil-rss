package cedilrss

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const (
	defaultSourceURL       = "https://cedil.cesa.or.jp/news"
	defaultOutputDir       = "public"
	defaultSiteURL         = "https://a-kaibu.github.io/cedil-rss/"
	defaultFeedTitle       = "CEDiL 新着資料"
	defaultFeedDescription = "CEDiL - CEDEC Digital Library の新着資料"
)

var (
	sessionPathRE = regexp.MustCompile(`^/cedil_sessions/view/\d+$`)
	newsDateRE    = regexp.MustCompile(`(\d{4})年(\d{1,2})月(\d{1,2})日`)
	jst           = time.FixedZone("JST", 9*60*60)
)

type Config struct {
	SourceURL       string
	OutputDir       string
	SiteURL         string
	FeedTitle       string
	FeedDescription string
	Now             func() time.Time
	Client          *http.Client
}

type Entry struct {
	Title     string
	Link      string
	Published time.Time
}

func DefaultConfig() Config {
	return Config{
		SourceURL:       defaultSourceURL,
		OutputDir:       defaultOutputDir,
		SiteURL:         defaultSiteURL,
		FeedTitle:       defaultFeedTitle,
		FeedDescription: defaultFeedDescription,
		Now:             time.Now,
		Client:          &http.Client{Timeout: 30 * time.Second},
	}
}

func Run(ctx context.Context, cfg Config) error {
	cfg = withDefaults(cfg)

	body, err := fetch(ctx, cfg.Client, cfg.SourceURL)
	if err != nil {
		return err
	}

	entries, err := ParseNews(bytes.NewReader(body), cfg.SourceURL)
	if err != nil {
		return err
	}

	rss, err := BuildRSS(entries, cfg)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(cfg.OutputDir, "index.xml")
	if err := os.WriteFile(path, rss, 0o644); err != nil {
		return err
	}
	return nil
}

func ParseNews(r io.Reader, sourceURL string) ([]Entry, error) {
	root, err := html.Parse(r)
	if err != nil {
		return nil, err
	}

	base, err := url.Parse(sourceURL)
	if err != nil {
		return nil, err
	}

	blocks := findNewsBlocks(root)
	entries := make([]Entry, 0)
	seen := map[string]bool{}
	for _, block := range blocks {
		published, ok := dateFromBlock(block)
		if !ok {
			continue
		}

		for _, anchor := range anchors(block) {
			href := attr(anchor, "href")
			linkURL, ok := sessionURL(base, href)
			if !ok || seen[linkURL] {
				continue
			}
			title := normalizeText(textContent(anchor))
			if title == "" {
				continue
			}
			seen[linkURL] = true
			entries = append(entries, Entry{
				Title:     title,
				Link:      linkURL,
				Published: published,
			})
		}
	}

	if len(entries) == 0 {
		return nil, errors.New("no CEDiL news entries found")
	}

	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Published.After(entries[j].Published)
	})
	return entries, nil
}

func BuildRSS(entries []Entry, cfg Config) ([]byte, error) {
	cfg = withDefaults(cfg)
	siteURL, err := normalizeSiteURL(cfg.SiteURL)
	if err != nil {
		return nil, err
	}

	now := cfg.Now().In(jst)
	feedURL := siteURL + "index.xml"
	items := make([]rssItem, 0, len(entries))
	for _, entry := range entries {
		items = append(items, rssItem{
			Title:   entry.Title,
			Link:    entry.Link,
			GUID:    rssGUID{IsPermaLink: "true", Value: entry.Link},
			PubDate: entry.Published.In(jst).Format(time.RFC1123Z),
		})
	}

	doc := rssDocument{
		Version: "2.0",
		Atom:    "http://www.w3.org/2005/Atom",
		Channel: rssChannel{
			Title:       cfg.FeedTitle,
			Link:        defaultSourceURL,
			Description: cfg.FeedDescription,
			Language:    "ja",
			LastBuild:   now.Format(time.RFC1123Z),
			AtomLink: atomLink{
				Href: feedURL,
				Rel:  "self",
				Type: "application/rss+xml",
			},
			Items: items,
		},
	}

	out, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), out...), nil
}

func fetch(ctx context.Context, client *http.Client, sourceURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "cedil-rss/1.0 (+https://github.com/)")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch %s: unexpected status %s", sourceURL, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func findNewsBlocks(n *html.Node) []*html.Node {
	var out []*html.Node
	var walk func(*html.Node)
	walk = func(cur *html.Node) {
		if cur.Type == html.ElementNode && cur.Data == "div" && attr(cur, "id") == "session_detail_list" {
			out = append(out, cur)
		}
		for child := cur.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return out
}

func dateFromBlock(block *html.Node) (time.Time, bool) {
	for child := block.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && child.Data == "h2" {
			return parseJapaneseDate(textContent(child))
		}
	}
	return time.Time{}, false
}

func parseJapaneseDate(s string) (time.Time, bool) {
	m := newsDateRE.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, false
	}
	year, _ := strconv.Atoi(m[1])
	month, _ := strconv.Atoi(m[2])
	day, _ := strconv.Atoi(m[3])
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, jst), true
}

func anchors(n *html.Node) []*html.Node {
	var out []*html.Node
	var walk func(*html.Node)
	walk = func(cur *html.Node) {
		if cur.Type == html.ElementNode && cur.Data == "a" {
			out = append(out, cur)
		}
		for child := cur.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return out
}

func sessionURL(base *url.URL, href string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return "", false
	}
	absolute := base.ResolveReference(u)
	if absolute.Scheme != "https" || absolute.Host != "cedil.cesa.or.jp" {
		return "", false
	}
	if !sessionPathRE.MatchString(absolute.Path) {
		return "", false
	}
	absolute.RawQuery = ""
	absolute.Fragment = ""
	return absolute.String(), true
}

func textContent(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(cur *html.Node) {
		if cur.Type == html.TextNode {
			b.WriteString(cur.Data)
			b.WriteByte(' ')
		}
		for child := cur.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return b.String()
}

func normalizeText(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "\u00a0", " ")), " ")
}

func normalizeSiteURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("SITE_URL must be absolute: %q", raw)
	}
	u.RawQuery = ""
	u.Fragment = ""
	out := u.String()
	if !strings.HasSuffix(out, "/") {
		out += "/"
	}
	return out, nil
}

func withDefaults(cfg Config) Config {
	if cfg.SourceURL == "" {
		cfg.SourceURL = defaultSourceURL
	}
	if cfg.OutputDir == "" {
		cfg.OutputDir = defaultOutputDir
	}
	if cfg.SiteURL == "" {
		cfg.SiteURL = defaultSiteURL
	}
	if cfg.FeedTitle == "" {
		cfg.FeedTitle = defaultFeedTitle
	}
	if cfg.FeedDescription == "" {
		cfg.FeedDescription = defaultFeedDescription
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: 30 * time.Second}
	}
	return cfg
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

type rssDocument struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Atom    string     `xml:"xmlns:atom,attr"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title       string    `xml:"title"`
	Link        string    `xml:"link"`
	Description string    `xml:"description"`
	Language    string    `xml:"language"`
	LastBuild   string    `xml:"lastBuildDate"`
	AtomLink    atomLink  `xml:"atom:link"`
	Items       []rssItem `xml:"item"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr"`
}

type rssItem struct {
	Title   string  `xml:"title"`
	Link    string  `xml:"link"`
	GUID    rssGUID `xml:"guid"`
	PubDate string  `xml:"pubDate"`
}

type rssGUID struct {
	IsPermaLink string `xml:"isPermaLink,attr"`
	Value       string `xml:",chardata"`
}
