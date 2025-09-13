// Copyright (C) 2021 - 2025 PurpleSec Team
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.
//

package crosspost

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-mastodon"
	"golang.org/x/net/html"
)

var replacer = strings.NewReplacer("</p><p>", "\n\n</p><p>", "<br />", "\n", "<br/>", "\n", "<br>", "\n", "@twitter.com", "")

type postData struct {
	_       [0]func()
	Media   []postMedia
	Content string
}
type postMedia struct {
	_                [0]func()
	Size             int64
	File, Text, Type string
}
type postAccount struct {
	_      [0]func()
	tw     *twClient
	blue   *blueClient
	http   *http.Client
	name   string
	repl   *strings.Replacer
	user   mastodon.ID
	masto  *mastodon.Client
	parent *CrossPost
	prefix string
}

func (p postData) close() {
	for i := range p.Media {
		os.Remove(p.Media[i].File)
	}
}
func stripHTML(s string) string {
	var (
		t = html.NewTokenizer(strings.NewReader(s))
		b strings.Builder
		p = t.Token()
	)
	for b.Grow(len(s)); ; {
		switch t.Next() {
		case html.ErrorToken:
			return b.String()
		case html.StartTagToken:
			p = t.Token()
		case html.TextToken:
			if p.Data == "script" || p.Data == "style" {
				continue
			}
			b.WriteString(html.UnescapeString(string(t.Text())))
		}
	}
}
func makeStringWithPrefix(s, p string) string {
	var (
		o, k = len(s), len(p)
		b    strings.Builder
		c    int
	)
	b.Grow(o + k)
	for _, v := range []rune(s) {
		n, _ := b.WriteRune(v)
		if c += n; (c + k) >= 275 {
			break
		}
	}
	if c != o {
		b.WriteString("..")
	}
	if k > 0 {
		b.WriteByte(' ')
		b.WriteString(p)
	}
	return b.String()
}
func (p *postAccount) start(x context.Context, g *sync.WaitGroup) error {
	s, err := p.masto.StreamingUser(x)
	if err != nil {
		return err
	}
	p.parent.log.Info(`[poster/%s]: Starting receiver and listener threads..`, p.name)
	g.Go(func() {
		g.Add(1)
		o := make(chan postData, 16)
		go p.post(x, g, o)
		p.listen(x, o, s)
		p.parent.log.Debug(`[poster/%s]: Cleaning up..`, p.name)
		g.Done()
	})
	return nil
}
func (p *postAccount) post(x context.Context, g *sync.WaitGroup, o <-chan postData) {
	p.parent.log.Debug(`[poster/%s]: Starting receiver thread..`, p.name)
	for g.Add(1); ; {
		select {
		case <-x.Done():
			p.parent.log.Debug(`[poster/%s]: Stopping receiver thread..`, p.name)
			g.Done()
			return
		case d := <-o:
			/*if p.tw != nil {
				p.parent.log.Trace(`[poster/%s]: Sending post to Twitter poster..`, p.name)
				if err := p.tw.post(x, &d); err != nil {
					p.parent.log.Debug(`[poster/%s]: Twitter post failed: %s!`, p.name, err.Error())
				}
			}*/
			if p.blue != nil {
				p.parent.log.Trace(`[poster/%s]: Sending post to BlueSky poster..`, p.name)
				if err := p.blue.post(x, &d); err != nil {
					p.parent.log.Debug(`[poster/%s]: BlueSky post failed: %s!`, p.name, err.Error())
				}
			}
			d.close()
		}
	}
}
func (p *postAccount) handle(x context.Context, o chan<- postData, e *mastodon.Status) {
	p.parent.log.Trace(`[poster/%s]: Received status "%s" from stream..`, p.name, e.ID)
	if e.Account.ID != p.user {
		p.parent.log.Debug(`[poster/%s]: Ignoring status from "%s" as it's not from "%s"..`, p.name, e.Account.ID, p.user)
		return
	}
	if e.Visibility != "direct" || e.Reblog != nil || e.InReplyToID != nil || e.InReplyToAccountID != nil || len(e.Content) == 0 {
		p.parent.log.Debug(`[poster/%s]: Ignoring status from "%s" as it does not match the content criteria..`, p.name, e.ID)
		return
	}
	m, err := p.download(x, e.ID, e.MediaAttachments)
	if err != nil {
		p.parent.log.Error(`[poster/%s/%s]: Cannot download attachments: %s!`, p.name, e.ID, err.Error())
		return
	}
	s := stripHTML(replacer.Replace(e.Content))
	if p.repl != nil {
		s = p.repl.Replace(s)
	}
	var k string
	if len(p.prefix) > 0 {
		k = p.prefix + "/" + string(e.ID)
	}
	o <- postData{Content: makeStringWithPrefix(s, k), Media: m}
	p.parent.log.Debug(`[poster/%s/%s]: Sent post to receivers!`, p.name, e.ID)
}
func (c *CrossPost) newPostAccount(x context.Context, a *account, d time.Duration) error {
	m := mastodon.NewClient(&mastodon.Config{
		Server:       a.Mastodon.Server,
		ClientID:     a.Mastodon.ClientID,
		ClientSecret: a.Mastodon.ClientSecret,
		AccessToken:  a.Mastodon.AccessToken,
	})
	v, err := m.GetAccountCurrentUser(x)
	if err != nil {
		return errors.New("mastodon client setup failed: " + err.Error())
	}
	p := &postAccount{
		tw:     nil,
		blue:   nil,
		user:   v.ID,
		name:   v.Username,
		masto:  m,
		prefix: a.Prefix,
		parent: c,
		http: &http.Client{
			Timeout: d,
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				DialContext:           (&net.Dialer{Timeout: d, KeepAlive: time.Second * 30}).DialContext,
				MaxIdleConns:          256,
				IdleConnTimeout:       time.Second * 60,
				DisableKeepAlives:     false,
				ForceAttemptHTTP2:     true,
				TLSHandshakeTimeout:   d,
				ExpectContinueTimeout: d,
				ResponseHeaderTimeout: d,
			},
		}}
	if err = p.newBlue(a.Blue, d, p.http); err != nil {
		return err
	}
	if err = p.newTwitter(a.Twitter, d, p.http); err != nil {
		return err
	}
	if len(a.Replace) > 0 {
		r := make([]string, 0, len(a.Replace)*2)
		for k, v := range a.Replace {
			r = append(r, k, v)
		}
		p.repl = strings.NewReplacer(r...)
	}
	c.accounts = append(c.accounts, p)
	return nil
}
func (p *postAccount) listen(x context.Context, o chan<- postData, i <-chan mastodon.Event) {
	p.parent.log.Debug(`[poster/%s]: Starting listener thread..`, p.name)
	for {
		select {
		case <-x.Done():
			p.parent.log.Debug(`[poster/%s]: Stopping listener thread..`, p.name)
			return
		case e := <-i:
			switch v := e.(type) {
			case *mastodon.ErrorEvent:
				p.parent.log.Error(`[poster/%s]: Received an error from the stream: %s!`, p.name, v.Err.Error())
			case *mastodon.UpdateEvent:
				p.handle(x, o, v.Status)
			default:
			}
		}
	}
}
func (p *postAccount) download(x context.Context, i mastodon.ID, m []mastodon.Attachment) ([]postMedia, error) {
	if len(m) == 0 {
		return nil, nil
	}
	p.parent.log.Debug(`[poster/%s/%s]: Processing attachments..`, p.name, i)
	a := make([]postMedia, 0, len(m))
	for _, v := range m {
		if len(v.URL) == 0 || len(v.Type) == 0 || (v.Type != "image" && v.Type != "gif" && v.Type != "video") {
			continue
		}
		f, err := os.CreateTemp("", "crosspost-media-*")
		if err != nil {
			return nil, errors.New(`media temp creation failed: ` + err.Error())
		}
		var (
			r, _ = http.NewRequestWithContext(x, "GET", v.URL, nil)
			k    = postMedia{File: f.Name(), Text: v.Description}
			d    bool
		)
		switch v.Type {
		case "gif", "video":
			k.Type = "video/mp4"
		default:
			k.Type = "image/jpeg"
		}
		p.parent.log.Debug(`[poster/%s/%s]: Downloading attachment "%s" (%s) into "%s"..`, p.name, i, v.URL, v.Type, k.File)
		if o, err := p.http.Do(r); err == nil {
			if o.Body != nil {
				if k.Size, err = io.Copy(f, o.Body); err == nil {
					d = true
				} else {
					p.parent.log.Error(`[poster/%s/%s]: Cannot download attachment from "%s" into file "%s": %s!`, p.name, i, v.URL, k.File, err.Error())
				}
			}
		} else {
			p.parent.log.Error(`[poster/%s/%s]: Cannot download attachment from "%s": %s!`, p.name, i, v.URL, k.File, err.Error())
		}
		if f.Close(); !d {
			os.Remove(k.File)
			continue
		}
		p.parent.log.Debug(`[poster/%s/%s]: Download of attachment "%s" into "%s" completed successfully!`, p.name, i, v.URL, k.File)
		if len(v.Type) == 0 {
			v.Type = "image/jpeg"
		}
		a = append(a, k)
	}
	p.parent.log.Debug(`[poster/%s/%s]: Processed %d attachments.`, p.name, i, len(a))
	return a, nil
}
