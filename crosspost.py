#!/usr/bin/python
# CrossPost
#  Post Mastodon Posts to Twitter.
#
# Copyright (C) 2023 iDigitalFlame
#
# This program is free software: you can redistribute it and/or modify
# it under the terms of the GNU General Public License as published by
# the Free Software Foundation, either version 3 of the License, or
# any later version.
#
# This program is distributed in the hope that it will be useful,
# but WITHOUT ANY WARRANTY; without even the implied warranty of
# MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
# GNU General Public License for more details.
#
# You should have received a copy of the GNU General Public License
# along with this program.  If not, see <https://www.gnu.org/licenses/>.
#

from requests import get
from tempfile import mkstemp
from bs4 import BeautifulSoup
from os import fdopen, remove
from sys import exit, stderr, argv
from json import loads, JSONDecodeError
from mastodon import Mastodon, StreamListener
from tweepy import Client, OAuth1UserHandler, API
from signal import sigwait, SIGINT, SIGKILL, SIGSTOP


HELP_TEXT = """CrossPost v1 - Post Mastodon Posts to Twitter

Usage:
    {bin} <config_file>

Pass a configuration file that contains the JSON configuration for CrossPost.

JSON Configuration Example:
{{
    "accounts": [
        {{
            "mastodon": {{
                "server": "https://<instance_url>",
                "client_id": "client_id_value",
                "client_secret": "client_secret_value",
                "access_token": "access_token_value"
            }},
            "twitter": {{
                "consumer_key": "consumer_key_value",
                "consumer_secret": "consumer_secret_value",
                "access_token": "access_token_value",
                "access_secret": "access_secret_value"
            }}
        }},
        {{
            "mastodon": {{
                "server": "https://<instance_url>",
                "client_id": "client_id_value",
                "client_secret": "client_secret_value",
                "access_token": "access_token_value"
            }},
            "twitter": {{
                "consumer_key": "consumer_key_value",
                "consumer_secret": "consumer_secret_value",
                "access_token": "access_token_value",
                "access_secret": "access_secret_value"
            }},
            "prefix": "<prefix_short_url>/post/"
        }}
    ]
}}

The only optional value is the "prefix" value which takes a URL value that will
be appended to Tweets (if the char limit allows!) with the Mastodon post ID. This
can be used as a quasi-link shortener.
"""


class Twitter(object):
    __slots__ = ("v1", "v2")

    def __init__(self, consumer_key, consumer_secret, access_token, access_secret):
        self.v1 = API(
            OAuth1UserHandler(
                consumer_key=consumer_key,
                consumer_secret=consumer_secret,
                access_token=access_token,
                access_token_secret=access_secret,
            )
        )
        self.v2 = Client(
            consumer_key=consumer_key,
            consumer_secret=consumer_secret,
            access_token=access_token,
            access_token_secret=access_secret,
        )

    def post_tweet(self, text, media=None):
        if isinstance(media, list) and len(media) > 0:
            a = list()
            for e in media:
                a.append(self.v1.media_upload(e).media_id)
        else:
            a = None
        return self.v2.create_tweet(text=text, media_ids=a)[0]["id"]


class CrossPoster(StreamListener):
    __slots__ = ("handle", "mastodon", "prefix", "twitter", "uid", "name")

    def __init__(self, twitter, mastodon, prefix=None):
        StreamListener.__init__(self)
        self.handle = None
        self.prefix = prefix
        self.twitter = twitter
        self.mastodon = mastodon
        a = mastodon.account_verify_credentials()
        self.uid = a["id"]
        self.name = a["acct"]
        del a

    def close(self):
        if self.handle is None:
            return
        self.handle.close()
        self.handle = None

    def start(self):
        self.handle = self.mastodon.stream_user(
            self, run_async=True, reconnect_async=True
        )

    def on_update(self, x):
        if self.twitter is None:
            return
        if x["account"]["id"] != self.uid:
            return
        if x["visibility"] != "public":
            return
        if x["in_reply_to_id"] is not None or x["in_reply_to_account_id"] is not None:
            return
        print(f'[{self.name}] Received Post "{x["id"]}" by "@{x["account"]["acct"]}"..')
        try:
            m = _get_media(x.get("media_attachments"))
        except OSError as err:
            return print(
                f"[{self.name}] Could not download media attachments: {err}!",
                file=stderr,
            )
        c = BeautifulSoup(
            x["content"]
            .replace("</p><p>", "\n\n</p><p>")
            .replace("<br />", "\n")
            .replace("<br/>", "\n")
            .replace("<br>", "\n"),
            features="html.parser",
        ).text.replace("@twitter.com", "")
        if len(c) > 280:
            c = c[276] + " ..."
        if isinstance(self.prefix, str) and len(self.prefix) > 0:
            v = self.prefix + "/" + str(x["id"])
            if len(c) + len(v) + 1 <= 280:
                c = c + "\n" + v
            del v
        try:
            t = self.twitter.post_tweet(c, m)
        except Exception as err:
            return print(f"[{self.name}] Could not post Tweet: {err}!", file=stderr)
        print(f'[{self.name}] Posted Tweet "{t}"!')
        if not isinstance(m, list) or len(m) == 0:
            return
        for i in m:
            try:
                remove(i)
            except OSError as err:
                print(err)
        del m


def _get_media(media):
    if not isinstance(media, list) or len(media) == 0:
        return None
    r = list()
    for e in media:
        i, n = mkstemp(text=False)
        with fdopen(i, mode="wb") as f:
            with get(e["url"], stream=True) as x:
                f.write(x.content)
        r.append(n)
    return r


def _parse_and_load(config):
    if not isinstance(config, dict) or len(config) == 0:
        raise ValueError("bad config entry")
    if "twitter" not in config:
        raise ValueError('missing "twitter" entry')
    if "mastodon" not in config:
        raise ValueError('missing "mastodon" entry')
    if not isinstance(config["twitter"], dict) or len(config["twitter"]) == 0:
        raise ValueError('"twitter" entry is invalid')
    if not isinstance(config["mastodon"], dict) or len(config["mastodon"]) == 0:
        raise ValueError('"mastodon" entry is invalid')
    if "server" not in config["mastodon"]:
        raise ValueError('missing "server" in "mastodon" entry')
    if "client_id" not in config["mastodon"]:
        raise ValueError('missing "client_id" in "mastodon" entry')
    if "client_secret" not in config["mastodon"]:
        raise ValueError('missing "client_secret" in "mastodon" entry')
    if "access_token" not in config["mastodon"]:
        raise ValueError('missing "access_token" in "mastodon" entry')
    if "consumer_key" not in config["twitter"]:
        raise ValueError('missing "consumer_key" in "twitter" entry')
    if "consumer_secret" not in config["twitter"]:
        raise ValueError('missing "consumer_secret" in "twitter" entry')
    if "access_token" not in config["twitter"]:
        raise ValueError('missing "access_token" in "twitter" entry')
    if "access_secret" not in config["twitter"]:
        raise ValueError('missing "access_secret" in "twitter" entry')
    m = Mastodon(
        api_base_url=config["mastodon"]["server"],
        client_id=config["mastodon"]["client_id"],
        client_secret=config["mastodon"]["client_secret"],
        access_token=config["mastodon"]["access_token"],
    )
    t = Twitter(
        config["twitter"]["consumer_key"],
        config["twitter"]["consumer_secret"],
        config["twitter"]["access_token"],
        config["twitter"]["access_secret"],
    )
    return CrossPoster(t, m, config.get("prefix"))


if __name__ == "__main__":
    if len(argv) <= 1:
        print(HELP_TEXT.format(bin=argv[0]))
        exit(2)
    try:
        with open(argv[1], "r") as f:
            c = loads(f.read())
    except OSError as err:
        print(f'Cannot open "{argv[1]}": {err}', file=stderr)
        exit(1)
    except JSONDecodeError as err:
        print(f'Cannot parse "{argv[1]}": {err}', file=stderr)
        exit(1)
    if not isinstance(c, dict) or "accounts" not in c:
        print(f'Bad configuration file "{argv[1]}"!', file=stderr)
        exit(1)
    if len(c["accounts"]) == 0:
        print(f'No accounts found in "{argv[1]}"!', file=stderr)
        exit(1)
    e = list()
    for a in c["accounts"]:
        try:
            e.append(_parse_and_load(a))
        except Exception as err:
            print(f'Cannot parse/load config entry in "{argv[1]}": {err}!')
            exit(1)
    del c
    print("Starting CrossPost threads..")
    for i in e:
        try:
            i.start()
        except Exception as err:
            print(f'Cannot start CrossPost thread for "{i.user}": {err}!')
            exit(1)
    print(f"Started {len(e)} CrossPost threads!")
    sigwait([SIGINT, SIGKILL, SIGSTOP])
    print("Closing threads..")
    for i in e:
        try:
            i.close()
        except Exception as err:
            print(f'Cannot close Cross thread for "{i.user}": {err}!')
    del e
