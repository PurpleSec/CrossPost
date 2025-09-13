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
	"errors"
	"strconv"
	"time"
)

// Defaults is a string representation of a JSON formatted default configuration
// for a Crosspost instance.
const Defaults = `{
    "accounts": [
        {
            "mastodon": {
                "server": "https://<instance_url>",
                "client_id": "client_id_value",
                "client_secret": "client_secret_value",
                "access_token": "access_token_value"
            },
            "bluesky": {
                "username": "bluesky_email",
                "password": "bluesky_app_password"
            },
            "twitter": {
                "consumer_key": "consumer_key_value",
                "consumer_secret": "consumer_secret_value",
                "access_token": "access_token_value",
                "access_secret": "access_secret_value"
            },
            "replace": {
                "emoji1": "emoji",
                "emoji2": "emoji",
                "emoji3": "emoji",
            }
        },
        {
            "mastodon": {
                "server": "https://<instance_url>",
                "client_id": "client_id_value",
                "client_secret": "client_secret_value",
                "access_token": "access_token_value"
            },
            "twitter": {
                "consumer_key": "consumer_key_value",
                "consumer_secret": "consumer_secret_value",
                "access_token": "access_token_value",
                "access_secret": "access_secret_value"
            },
            "prefix": "<prefix_short_url>/post/"
        }
    ]
}
`

const blueDefaultServer = "bsky.social"

type log struct {
	File  string `json:"file"`
	Level int    `json:"level"`
}
type config struct {
	Log      log           `json:"log"`
	Timeout  time.Duration `json:"timeout"`
	Accounts []account     `json:"accounts"`
}
type account struct {
	Blue     *accountBlue      `json:"bluesky"`
	Prefix   string            `json:"prefix"`
	Replace  map[string]string `json:"replace"`
	Twitter  *accountTwitter   `json:"twitter"`
	Mastodon *accountMastodon  `json:"mastodon"`
}
type accountBlue struct {
	Server   string `json:"server"`
	Username string `json:"username"`
	Password string `json:"password"`
}
type accountTwitter struct {
	AccessToken    string `json:"access_token"`
	AccessSecret   string `json:"access_secret"`
	ConsumerKey    string `json:"consumer_key"`
	ConsumerSecret string `json:"consumer_secret"`
}
type accountMastodon struct {
	Server       string `json:"server"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	AccessToken  string `json:"access_token"`
}

func (c *config) check() error {
	if len(c.Accounts) == 0 {
		return errors.New("no accounts specified in config")
	}
	for i := range c.Accounts {
		if c.Accounts[i].Mastodon == nil {
			return errors.New(`account at "` + strconv.Itoa(i) + `" is missing "mastodon"`)
		}
		if c.Accounts[i].Blue == nil && c.Accounts[i].Twitter == nil {
			return errors.New(`account at "` + strconv.Itoa(i) + `" does not have a "bluesky" or "twitter" entry`)
		}
		if len(c.Accounts[i].Mastodon.Server) == 0 {
			return errors.New(`account at "` + strconv.Itoa(i) + `" has a missing or empty "mastodon"->"server" entry`)
		}
		if len(c.Accounts[i].Mastodon.ClientID) == 0 {
			return errors.New(`account at "` + strconv.Itoa(i) + `" has a missing or empty "mastodon"->"client_id" entry`)
		}
		if len(c.Accounts[i].Mastodon.ClientSecret) == 0 {
			return errors.New(`account at "` + strconv.Itoa(i) + `" has a missing or empty "mastodon"->"client_secret" entry`)
		}
		if len(c.Accounts[i].Mastodon.AccessToken) == 0 {
			return errors.New(`account at "` + strconv.Itoa(i) + `" has a missing or empty "mastodon"->"access_token" entry`)
		}
		if c.Accounts[i].Blue != nil {
			if len(c.Accounts[i].Blue.Server) == 0 {
				c.Accounts[i].Blue.Server = blueDefaultServer
			}
			if len(c.Accounts[i].Blue.Username) == 0 {
				return errors.New(`account at "` + strconv.Itoa(i) + `" has a missing or empty "bluesky"->"username" entry`)
			}
			if len(c.Accounts[i].Blue.Password) == 0 {
				return errors.New(`account at "` + strconv.Itoa(i) + `" has a missing or empty "bluesky"->"password" entry`)
			}
		}
		if c.Accounts[i].Twitter != nil {
			if len(c.Accounts[i].Twitter.AccessToken) == 0 {
				return errors.New(`account at "` + strconv.Itoa(i) + `" has a missing or empty "twitter"->"access_token" entry`)
			}
			if len(c.Accounts[i].Twitter.AccessSecret) == 0 {
				return errors.New(`account at "` + strconv.Itoa(i) + `" has a missing or empty "twitter"->"access_secret" entry`)
			}
			if len(c.Accounts[i].Twitter.ConsumerKey) == 0 {
				return errors.New(`account at "` + strconv.Itoa(i) + `" has a missing or empty "twitter"->"consumer_key" entry`)
			}
			if len(c.Accounts[i].Twitter.ConsumerSecret) == 0 {
				return errors.New(`account at "` + strconv.Itoa(i) + `" has a missing or empty "twitter"->"consumer_secret" entry`)
			}
		}
	}
	if c.Timeout == 0 {
		c.Timeout = time.Second * 5
	}
	return nil
}
