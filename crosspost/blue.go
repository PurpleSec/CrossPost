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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image/jpeg"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const sizeMax = int64(1_000_000)
const loginDelay = 2 * time.Hour

var (
	expTags     = regexp.MustCompile(`(#[^\d\s]\S*)`)
	expURLs     = regexp.MustCompile(`(https?:\/\/(www\.)?[-a-zA-Z0-9@:%._\+~#=]{1,256}\.[a-zA-Z0-9()]{1,6}\b([-a-zA-Z0-9()@:%_\+.~#?&//=]*[-a-zA-Z0-9@%_\+~#//=])?)`)
	expMentions = regexp.MustCompile(`@([a-zA-Z0-9-]{0,61}\.?[a-zA-Z0-9-]{0,30}\.?[a-zA-Z0-9-]{0,30})(\s|$)`)
)

type blueAuth struct {
	User     string `json:"identifier"`
	Password string `json:"password"`
}
type blueBlob struct {
	Link struct {
		ID string `json:"$link"`
	} `json:"ref"`
	Mime string  `json:"mimeType"`
	Size float64 `json:"size"`
	Type string  `json:"$type"`
}
type bluePost struct {
	ID     string `json:"repo"`
	Record struct {
		Type    string      `json:"$type"`
		Text    string      `json:"text"`
		Langs   []string    `json:"langs,omitempty"`
		Facets  []blueFacet `json:"facets,omitempty"`
		Created string      `json:"createdAt"`
		Embed   *blueEmbed  `json:"embed,omitempty"`
	} `json:"record"`
	Collection string `json:"collection"`
}
type blueEmbed struct {
	Type   string      `json:"$type,omitempty"`
	Images []blueMedia `json:"images,omitempty"`
}

type blueFacet struct {
	Index struct {
		End   int `json:"byteEnd"`
		Start int `json:"byteStart"`
	} `json:"index"`
	Features []blueFacetData `json:"features"`
}
type blueMedia struct {
	Alt   string    `json:"alt"`
	Image *blueBlob `json:"image"`
}
type blueClient struct {
	_      [0]func()
	id     string
	pw     string
	user   string
	last   time.Time
	token  string
	server string
	poster *postAccount
}
type blueFacetData struct {
	Did  string `json:"did,omitempty"`
	Tag  string `json:"tag,omitempty"`
	URL  string `json:"url,omitempty"`
	Type string `json:"$type"`
}
type blueAuthResponse struct {
	ID    string `json:"did"`
	Error string `json:"error"`
	Token string `json:"accessJwt"`
}
type bluePostResponse struct {
	ID    string `json:"cid"`
	Error string `json:"error"`
}
type blueMediaResponse struct {
	Blob  *blueBlob `json:"blob"`
	Error string    `json:"error"`
}
type blueSearchResponse struct {
	Actors []struct {
		ID     string `json:"did"`
		Handle string `json:"handle"`
	} `json:"actors"`
}

func resizeMedia(m *postMedia) (string, bool, error) {
	if m.Size < sizeMax {
		return m.File, false, nil
	}
	f, err := os.Open(m.File)
	if err != nil {
		return "", false, errors.New(`media open "` + m.File + `" failed: ` + err.Error())
	}
	i, err := jpeg.Decode(f)
	if f.Close(); err != nil {
		return "", false, errors.New(`media read "` + m.File + `" failed: ` + err.Error())
	}
	o, err := os.CreateTemp("", "crosspost-media-convert-*")
	if err != nil {
		return "", false, errors.New(`media temp creation failed: ` + err.Error())
	}
	var (
		q, c = 90, m.Size
		v    os.FileInfo
	)
	for q > 0 && c >= sizeMax {
		if err = o.Truncate(0); err != nil {
			break
		}
		if _, err = o.Seek(0, io.SeekStart); err != nil {
			break
		}
		if err = jpeg.Encode(o, i, &jpeg.Options{Quality: q}); err != nil {
			break
		}
		if v, err = o.Stat(); err != nil {
			break
		}
		if c = v.Size(); c >= sizeMax && q < 10 {
			err = errors.New(`media file "` + m.File + `" (` + strconv.FormatInt(m.Size, 10) + `b) cannot be resized smaller`)
			break
		}
		q -= 10
	}
	p := o.Name()
	o.Close()
	return p, true, err
}
func (c *blueClient) authenticate(x context.Context) error {
	if !c.last.IsZero() && time.Now().Sub(c.last) < loginDelay {
		return nil
	}
	var r blueAuthResponse
	if err := c.api(x, http.MethodPost, "com.atproto.server.createSession", "", blueAuth{User: c.user, Password: c.pw}, &r); err != nil {
		return err
	}
	if len(r.Error) > 0 {
		return errors.New("bluesky auth error: " + r.Error)
	}
	if len(r.ID) == 0 || len(r.Token) == 0 {
		return errors.New("bluesky auth error: invalid server response")
	}
	c.id, c.token, c.last = r.ID, r.Token, time.Now()
	return nil
}
func (c *blueClient) post(x context.Context, d *postData) error {
	if err := c.authenticate(x); err != nil {
		return err
	}
	c.poster.parent.log.Debug(`[poster/%s/bluesky]: Received post..`, c.poster.name)
	m := make([]blueMedia, 0, len(d.Media))
	if len(d.Media) > 0 {
		c.poster.parent.log.Debug(`[poster/%s/bluesky]: Post has media, processing %d attachments..`, c.poster.name, len(d.Media))
		for i := range d.Media {
			if strings.HasPrefix(d.Media[i].Type, "video/") {
				c.poster.parent.log.Debug(`[poster/%s/bluesky]: Skipping unsupported video attachment..`, c.poster.name)
				continue
			}
			r, err := c.postMedia(x, &d.Media[i])
			if err != nil {
				return errors.New("media initialize failed: " + err.Error())
			}
			m = append(m, blueMedia{Alt: d.Media[i].Text, Image: r})
		}
	}
	var p bluePost
	p.ID, p.Collection = c.id, "app.bsky.feed.post"
	p.Record.Langs = []string{"en-US"}
	p.Record.Type, p.Record.Text = "app.bsky.feed.post", d.Content
	p.Record.Facets = c.facetTags(d.Content, c.facetURLs(d.Content, c.facetMentions(x, d.Content)))
	p.Record.Created = time.Now().UTC().Format("2006-01-02T15:04:05.999999Z")
	if len(m) > 0 {
		p.Record.Embed = &blueEmbed{Type: "app.bsky.embed.images", Images: m}
	}
	c.poster.parent.log.Debug(`[poster/%s/bluesky]: Posting Skeet..`, c.poster.name)
	var r bluePostResponse
	if err := c.api(x, http.MethodPost, "com.atproto.repo.createRecord", "", p, &r); err != nil {
		return err
	}
	if len(r.Error) > 0 {
		return errors.New(r.Error)
	}
	c.poster.parent.log.Info(`[poster/%s/bluesky]: Posted Skeet "%s"!`, c.poster.name, r.ID)
	return nil
}
func (c *blueClient) findUser(x context.Context, n string) string {
	var i blueAuthResponse // It's the same struct format
	if err := c.api(x, http.MethodGet, "com.atproto.identity.resolveHandle?handle="+url.QueryEscape(n+".bsky.social"), "", nil, &i); err == nil && len(i.ID) > 0 {
		return i.ID
	}
	var (
		r, _   = http.NewRequestWithContext(x, http.MethodGet, "https://public.api.bsky.app/xrpc/app.bsky.actor.searchActors?q="+url.QueryEscape(n), nil)
		o, err = c.poster.http.Do(r)
	)
	if err != nil || o.Body == nil {
		return ""
	}
	var a blueSearchResponse
	err = json.NewDecoder(o.Body).Decode(&a)
	if o.Body.Close(); err != nil {
		return ""
	}
	d := strings.ToLower(n)
	for _, v := range a.Actors {
		if len(v.ID) == 0 {
			continue
		}
		if strings.HasPrefix(strings.ToLower(v.Handle), d) {
			return v.ID
		}
	}
	return ""
}
func (c *blueClient) facetTags(s string, r []blueFacet) []blueFacet {
	m := expTags.FindAllStringIndex(s, -1)
	if m == nil {
		return r
	}
	for _, v := range m {
		if len(v) == 0 || v[0] < 0 || v[1] > len(s) || v[0]+1 >= v[1] {
			continue
		}
		var f blueFacet
		f.Features = []blueFacetData{blueFacetData{Tag: s[v[0]+1 : v[1]], Type: "app.bsky.richtext.facet#tag"}}
		f.Index.End, f.Index.Start = v[1], v[0]
		r = append(r, f)
	}
	return r
}
func (c *blueClient) facetURLs(s string, r []blueFacet) []blueFacet {
	m := expURLs.FindAllStringIndex(s, -1)
	if m == nil {
		return r
	}
	for _, v := range m {
		if len(v) == 0 || v[0] < 0 || v[1] > len(s) {
			continue
		}
		var f blueFacet
		f.Features = []blueFacetData{blueFacetData{URL: s[v[0]:v[1]], Type: "app.bsky.richtext.facet#link"}}
		f.Index.End, f.Index.Start = v[1], v[0]
		r = append(r, f)
	}
	return r
}
func (c *blueClient) facetMentions(x context.Context, s string) []blueFacet {
	m := expMentions.FindAllStringIndex(s, -1)
	if m == nil {
		return nil
	}
	r := make([]blueFacet, 0, len(m))
	for _, v := range m {
		if len(v) == 0 || v[0] < 0 || v[1] > len(s) || v[0]+1 >= v[1] {
			continue
		}
		var f blueFacet
		f.Features = []blueFacetData{blueFacetData{Type: "app.bsky.richtext.facet#mention"}}
		if f.Features[0].Did = c.findUser(x, s[v[0]+1:v[1]-1]); len(f.Features[0].Did) == 0 {
			continue
		}
		f.Index.End, f.Index.Start = v[1], v[0]
		r = append(r, f)
	}
	return r
}
func (c *blueClient) postMedia(x context.Context, m *postMedia) (*blueBlob, error) {
	p, d, err := resizeMedia(m)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, errors.New(`media open "` + m.File + `" failed: ` + err.Error())
	}
	var r blueMediaResponse
	err = c.apiReader(x, http.MethodPost, "com.atproto.repo.uploadBlob", m.Type, f, &r)
	if f.Close(); d {
		os.Remove(p)
	}
	if err != nil {
		return nil, errors.New("media upload failed: " + err.Error())
	}
	if len(r.Error) > 0 {
		return nil, errors.New("media upload failed: " + r.Error)
	}
	c.poster.parent.log.Debug(`[poster/%s/bluesky]: Created MediaID "%s" from "%s"..`, c.poster.name, r.Blob.Link.ID, m.File)
	return r.Blob, nil
}
func (p *postAccount) newBlue(a *accountBlue, d time.Duration, h *http.Client) error {
	if a == nil {
		return nil
	}
	p.parent.log.Debug(`[poster/%s/bluesky]: Setting up BlueSky account..`, p.name)
	p.blue = &blueClient{
		id:     "",
		pw:     a.Password,
		user:   a.Username,
		last:   time.Time{},
		token:  "",
		server: a.Server,
		poster: p,
	}
	if strings.HasPrefix(a.Server, "http") && strings.IndexByte(a.Server, ':') <= 6 {
		p.blue.server = strings.TrimPrefix(strings.TrimPrefix(a.Server, "https://"), "http://")
	}
	if err := p.blue.authenticate(context.Background()); err != nil {
		return errors.New("bluesky client setup failed: " + err.Error())
	}
	p.parent.log.Info(`[poster/%s/bluesky]: BlueSKy account setup complete!`, p.name)
	return nil
}
func (c *blueClient) api(x context.Context, method, url, content string, input interface{}, output interface{}) error {
	if input != nil {
		var b bytes.Buffer
		if err := json.NewEncoder(&b).Encode(input); err != nil {
			return err
		}
		return c.apiReader(x, method, url, content, bytes.NewReader(b.Bytes()), output)
	}
	return c.apiReader(x, method, url, content, nil, output)
}
func (c *blueClient) apiReader(x context.Context, method, url, content string, reader io.Reader, output interface{}) error {
	r, _ := http.NewRequestWithContext(x, method, "https://"+c.server+"/xrpc/"+url, reader)
	if len(c.token) > 0 {
		r.Header.Set("Authorization", "Bearer "+c.token)
	}
	if len(content) > 0 {
		r.Header.Set("Content-Type", content)
	} else if reader != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	o, err := c.poster.http.Do(r)
	if err != nil {
		return err
	}
	if o.Body == nil {
		return nil
	}
	err = json.NewDecoder(o.Body).Decode(&output)
	o.Body.Close()
	return err
}
