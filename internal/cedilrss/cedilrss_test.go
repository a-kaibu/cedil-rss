package cedilrss

import (
	"encoding/xml"
	"strings"
	"testing"
	"time"
)

const fixtureNews = `
<!DOCTYPE html>
<html lang="ja"><body>
  <div id="session_detail_list">
    <h2>2026年1月21日 セッション資料を追加しました。</h2>
    <div class="new_article">
      <p>
        <p><a href="/cedil_sessions/view/3236">一致率で"聴く／聴かない"を判断：AIで変える音声チェック業務</a></p>
        <p><a href="/cedil_sessions/view/3237">ヘッドホンとDAWさえあれば、あなたもデスクで立体音響　~1から始めるDolby Atmos~</a></p>
      </p>
    </div>
  </div>
  <div id="session_detail_list">
    <h2>2025年11月18日 セッション資料を追加しました。</h2>
    <div class="new_article">
      <p>
        <a href="/cedil_sessions/view/3215">Godotからの教訓：オープンソースを活用して日本の技術的独立を取り戻す<br /></a>
        <a href="https://example.com/ignore">ignore</a>
      </p>
    </div>
  </div>
</body></html>`

func TestParseNews(t *testing.T) {
	entries, err := ParseNews(strings.NewReader(fixtureNews), defaultSourceURL)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 3 {
		t.Fatalf("len(entries) = %d, want 3", len(entries))
	}
	if entries[0].Title != `一致率で"聴く／聴かない"を判断：AIで変える音声チェック業務` {
		t.Fatalf("unexpected first title: %q", entries[0].Title)
	}
	if entries[0].Link != "https://cedil.cesa.or.jp/cedil_sessions/view/3236" {
		t.Fatalf("unexpected first link: %q", entries[0].Link)
	}
	if got := entries[0].Published.Format("2006-01-02"); got != "2026-01-21" {
		t.Fatalf("published = %s, want 2026-01-21", got)
	}
}

func TestParseNewsNoEntries(t *testing.T) {
	_, err := ParseNews(strings.NewReader(`<html><body></body></html>`), defaultSourceURL)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildRSS(t *testing.T) {
	entries := []Entry{{
		Title:     `A & B`,
		Link:      "https://cedil.cesa.or.jp/cedil_sessions/view/1",
		Published: time.Date(2026, 1, 21, 0, 0, 0, 0, jst),
	}}

	out, err := BuildRSS(entries, Config{
		SiteURL: "https://owner.github.io/cedil-rss",
		Now:     func() time.Time { return time.Date(2026, 5, 21, 12, 0, 0, 0, jst) },
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := xml.Unmarshal(out, new(any)); err != nil {
		t.Fatalf("invalid XML: %v\n%s", err, out)
	}
	xmlText := string(out)
	for _, want := range []string{
		`<title>A &amp; B</title>`,
		`<guid isPermaLink="true">https://cedil.cesa.or.jp/cedil_sessions/view/1</guid>`,
		`<pubDate>Wed, 21 Jan 2026 00:00:00 +0900</pubDate>`,
		`href="https://owner.github.io/cedil-rss/index.xml"`,
	} {
		if !strings.Contains(xmlText, want) {
			t.Fatalf("RSS does not contain %q:\n%s", want, xmlText)
		}
	}
}

func TestBuildRSSUsesEmbeddedSiteURL(t *testing.T) {
	out, err := BuildRSS([]Entry{{
		Title:     "A",
		Link:      "https://cedil.cesa.or.jp/cedil_sessions/view/1",
		Published: time.Date(2026, 1, 21, 0, 0, 0, 0, jst),
	}}, Config{
		Now: func() time.Time { return time.Date(2026, 5, 21, 12, 0, 0, 0, jst) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `href="https://a-kaibu.github.io/cedil-rss/index.xml"`) {
		t.Fatalf("RSS does not use embedded site URL:\n%s", out)
	}
}

func TestNormalizeSiteURLRequiresAbsoluteURL(t *testing.T) {
	if _, err := normalizeSiteURL("owner.github.io/repo"); err == nil {
		t.Fatal("expected error")
	}
}
