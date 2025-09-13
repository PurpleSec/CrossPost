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
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/michimani/gotwi"
	"github.com/michimani/gotwi/media/upload"
	"github.com/michimani/gotwi/media/upload/types"
	"github.com/michimani/gotwi/tweet/managetweet"
	tweet "github.com/michimani/gotwi/tweet/managetweet/types"
)

type twClient struct {
	_      [0]func()
	tw     *gotwi.Client
	poster *postAccount
}

func (c *twClient) post(x context.Context, d *postData) error {
	c.poster.parent.log.Debug(`[poster/%s/twitter]: Received post..`, c.poster.name)
	m := make([]string, 0, len(d.Media))
	if len(d.Media) > 0 {
		c.poster.parent.log.Debug(`[poster/%s/twitter]: Post has media, processing %d attachments..`, c.poster.name, len(d.Media))
		for i := range d.Media {
			r, err := c.postMedia(x, &d.Media[i])
			if err != nil {
				return errors.New("media initialize failed: " + err.Error())
			}
			m = append(m, r)
		}
	}
	var v *tweet.CreateInputMedia
	if len(m) > 0 {
		v = &tweet.CreateInputMedia{MediaIDs: m}
	}
	c.poster.parent.log.Debug(`[poster/%s/twitter]: Posting Tweet..`, c.poster.name)
	r, err := managetweet.Create(x, c.tw, &tweet.CreateInput{Text: &d.Content, Media: v})
	if err != nil {
		return err
	}
	c.poster.parent.log.Info(`[poster/%s/twitter]: Posted Tweet "%s"!`, c.poster.name, *r.Data.ID)
	return nil
}
func (c *twClient) postMedia(x context.Context, m *postMedia) (string, error) {
	t := types.MediaCategoryTweetImage
	if strings.HasPrefix(m.Type, "video/") {
		t = types.MediaCategoryTweetVideo
	}
	i, err := upload.Initialize(x, c.tw, &types.InitializeInput{
		Shared:        false,
		MediaType:     types.MediaType(m.Type),
		TotalBytes:    int(m.Size),
		MediaCategory: t,
	})
	if err != nil {
		return "", errors.New("media initialize failed: " + err.Error())
	}
	f, err := os.Open(m.File)
	if err != nil {
		return "", errors.New(`media open "` + m.File + `" failed: ` + err.Error())
	}
	_, err = upload.Append(x, c.tw, &types.AppendInput{
		Media:        f,
		MediaID:      i.Data.MediaID,
		SegmentIndex: 0,
	})
	if f.Close(); err != nil {
		return "", errors.New("media append failed: " + err.Error())
	}
	if _, err = upload.Finalize(x, c.tw, &types.FinalizeInput{MediaID: i.Data.MediaID}); err != nil {
		return "", errors.New("media finalize failed: " + err.Error())
	}
	c.poster.parent.log.Debug(`[poster/%s/twitter]: Created MediaID "%s" from "%s"..`, c.poster.name, i.Data.MediaID, m.File)
	return i.Data.MediaID, nil
}
func (p *postAccount) newTwitter(a *accountTwitter, d time.Duration, h *http.Client) error {
	if a == nil {
		return nil
	}
	p.parent.log.Debug(`[poster/%s/twitter]: Setting up Twitter account..`, p.name)
	t, err := gotwi.NewClient(&gotwi.NewClientInput{
		OAuthToken:           a.AccessToken,
		OAuthTokenSecret:     a.AccessSecret,
		APIKey:               a.ConsumerKey,
		APIKeySecret:         a.ConsumerSecret,
		AuthenticationMethod: gotwi.AuthenMethodOAuth1UserContext,
		HTTPClient:           h,
	})
	if err != nil {
		return errors.New("twitter client setup failed: " + err.Error())
	}
	p.tw = &twClient{tw: t, poster: p}
	p.parent.log.Info(`[poster/%s/twitter]: Twitter account setup complete!`, p.name)
	return nil
}
