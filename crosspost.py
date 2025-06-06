#!/usr/bin/python3
#
# CrossPost
#  Post Mastodon Posts to Twitter and BlueSky
#
# Copyright (C) 2025 iDigitalFlame
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

import threading

from re import compile
from os.path import isfile
from tempfile import mkstemp
from bs4 import BeautifulSoup
from mimetypes import guess_type
from requests import get, session
from sys import exit, stderr, argv
from urllib.request import getproxies
from json import loads, JSONDecodeError
from datetime import datetime, timezone
from os import fdopen, remove, kill, getpid
from mastodon import Mastodon, StreamListener
from tweepy import Client, OAuth1UserHandler, API
from signal import sigwait, SIGINT, SIGKILL, SIGSTOP

URLS = compile(
    rb"(https?:\/\/(www\.)?[-a-zA-Z0-9@:%._\+~#=]{1,256}\.[a-zA-Z0-9()]{1,6}\b([-a-zA-Z0-9()@:%_\+.~#?&//=]*[-a-"
    + rb"zA-Z0-9@%_\+~#//=])?)"
)
TAGS = compile(rb"((#[^\d\s]\S*)(?=\s)?)")
PROXIES = getproxies()
MENTIONS = compile(rb"@([a-zA-Z0-9-]{0,61})(\s|$)")
HELP_TEXT = """CrossPost v2.6 - Post Mastodon Posts to Twitter (and BlueSky!)

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
            "bluesky": {{
                "username": "bluesky_email",
                "password": "bluesky_app_password"
            }},
            "twitter": {{
                "consumer_key": "consumer_key_value",
                "consumer_secret": "consumer_secret_value",
                "access_token": "access_token_value",
                "access_secret": "access_secret_value"
            }},
            "replace": {{
                "emoji1": "emoji",
                "emoji2": "emoji",
                "emoji3": "emoji",
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

The only optional values are the "prefix" and "replace" values.

Prefix which takes a URL value that will be appended to Tweets (if the char limit
allows!) with the Mastodon post ID. This can be used as a quasi-link shortener.

Replace will replace the specified string matching phrases with the specified string
or character (or emoji!). These are case sensitive.
"""


def _get_media(media):
    if not isinstance(media, list) or len(media) == 0:
        return None
    r = list()
    for e in media:
        i, n = mkstemp(text=False)
        with fdopen(i, mode="wb") as f:
            with get(e["url"], stream=True, proxies=PROXIES) as x:
                f.write(x.content)
        r.append(n)
    return r


def _except_hook(args):
    print(f"Received an uncaught Thread error {args.exc_type} ({args.exc_value})!")
    kill(getpid(), SIGINT)


def _parse_and_load(config):
    if not isinstance(config, dict) or len(config) == 0:
        raise ValueError("bad config entry")
    if "mastodon" not in config:
        raise ValueError('missing "mastodon" entry')
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
    t, b = None, None
    if "twitter" in config:
        if not isinstance(config["twitter"], dict) or len(config["twitter"]) == 0:
            raise ValueError('"twitter" entry is invalid')
        if "consumer_key" not in config["twitter"]:
            raise ValueError('missing "consumer_key" in "twitter" entry')
        if "consumer_secret" not in config["twitter"]:
            raise ValueError('missing "consumer_secret" in "twitter" entry')
        if "access_token" not in config["twitter"]:
            raise ValueError('missing "access_token" in "twitter" entry')
        if "access_secret" not in config["twitter"]:
            raise ValueError('missing "access_secret" in "twitter" entry')
        t = Twitter(
            config["twitter"]["consumer_key"],
            config["twitter"]["consumer_secret"],
            config["twitter"]["access_token"],
            config["twitter"]["access_secret"],
        )
    if "bluesky" in config:
        if not isinstance(config["bluesky"], dict) or len(config["bluesky"]) == 0:
            raise ValueError('"bluesky" entry is invalid')
        if "username" not in config["bluesky"]:
            raise ValueError('missing "username" in "bluesky" entry')
        if "password" not in config["bluesky"]:
            raise ValueError('missing "password" in "bluesky" entry')
        b = BlueSky(config["bluesky"]["username"], config["bluesky"]["password"])
    if t is None and b is None:
        raise ValueError("missing and additional account object")
    m = Mastodon(
        api_base_url=config["mastodon"]["server"],
        client_id=config["mastodon"]["client_id"],
        client_secret=config["mastodon"]["client_secret"],
        access_token=config["mastodon"]["access_token"],
    )
    if PROXIES is not None and len(PROXIES) > 0:
        m.session.proxies = PROXIES
    return CrossPoster(t, b, m, config.get("prefix"), config.get("replace"))


class Twitter(object):
    __slots__ = ("_v1", "_v2")

    def __init__(self, consumer_key, consumer_secret, access_token, access_secret):
        self._v1 = API(
            OAuth1UserHandler(
                consumer_key=consumer_key,
                consumer_secret=consumer_secret,
                access_token=access_token,
                access_token_secret=access_secret,
            )
        )
        self._v2 = Client(
            consumer_key=consumer_key,
            consumer_secret=consumer_secret,
            access_token=access_token,
            access_token_secret=access_secret,
        )
        if PROXIES is not None and len(PROXIES) > 0:
            self._v1.session.proxies = PROXIES
            self._v2.session.proxies = PROXIES

    def post(self, text, media=None):
        if isinstance(media, list) and len(media) > 0:
            a = list()
            for e in media:
                a.append(self._v1.media_upload(e).media_id)
        else:
            a = None
        return self._v2.create_tweet(text=text, media_ids=a)[0]["id"]


class BlueSky(object):
    __slots__ = ("_did", "_jwt", "_req")

    def __init__(self, user, password):
        self._req = session()
        if PROXIES is not None and len(PROXIES) > 0:
            self._req.proxies = PROXIES
        self._authenticate(user, password)

    @staticmethod
    def _parse_urls(text):
        r = list()
        for m in URLS.finditer(text.encode("UTF-8")):
            r.append(
                {
                    "start": m.start(1),
                    "end": m.end(1),
                    "url": m.group(1).decode("UTF-8"),
                }
            )
        return r

    @staticmethod
    def _parse_tags(text):
        r = list()
        for m in TAGS.finditer(text.encode("UTF-8")):
            v = m.group(1).decode("UTF-8")
            if len(v) <= 1:
                continue
            r.append(
                {
                    "start": m.start(1),
                    "end": m.end(1),
                    "tag": v[1:],
                }
            )
            del v
        return r

    @staticmethod
    def _prase_mentions(text):
        r = list()
        for m in MENTIONS.finditer(text.encode("UTF-8")):
            v = m.start(1)
            if v > 0:
                v -= 1
            r.append(
                {
                    "start": v,
                    "end": m.end(1),
                    "handle": m.group(1).decode("UTF-8"),
                }
            )
            del v
        return r

    def _find_user(self, name):
        try:
            r = self._req.get(
                "https://bsky.social/xrpc/com.atproto.identity.resolveHandle",
                params={"handle": f"{name}.bsky.social"},
            )
            if r.status_code == 200:
                v = r.json()
                if "did" in v:
                    return v["did"]
                del v
        finally:
            r.close()
            del r
        try:
            r = self._req.get(
                "https://public.api.bsky.app/xrpc/app.bsky.actor.searchActors",
                params={"q": name},
            )
            if r.status_code != 200:
                return None
            v = r.json().get("actors")
            if not isinstance(v, list) or len(v) == 0:
                return None
            for i in v:
                if "did" not in i or "handle" not in i:
                    continue
                if i["handle"].lower().startswith(name.lower()):
                    return i["did"]
            del v
        finally:
            r.close()
            del r
        return None

    def _make_blob(self, image):
        if not isfile(image):
            raise ValueError(f'file "{image}" is not valid')
        with open(image, "rb") as f:
            t, _ = guess_type(image)
            if not isinstance(t, str) or len(t) == 0:
                t = "image/jpeg"
            r = self._req.post(
                "https://bsky.social/xrpc/com.atproto.repo.uploadBlob",
                headers={
                    "Content-Type": t,
                    "Authorization": f"Bearer {self._jwt}",
                },
                data=f.read(),
            )
            try:
                j = r.json()
            finally:
                r.close()
                del r
            del t
            if "error" in j:
                raise ValueError(j["error"])
            elif "blob" not in j:
                raise ValueError(
                    "xrpc/com.atproto.repo.uploadBlob: returned an invalid response"
                )
            return j["blob"]

    def _prase_facets(self, text):
        f = list()
        for m in BlueSky._prase_mentions(text):
            v = self._find_user(m["handle"])
            if v is None:
                continue
            f.append(
                {
                    "index": {
                        "byteStart": m["start"],
                        "byteEnd": m["end"],
                    },
                    "features": [
                        {
                            "$type": "app.bsky.richtext.facet#mention",
                            "did": v,
                        }
                    ],
                }
            )
            del v
        for u in BlueSky._parse_urls(text):
            f.append(
                {
                    "index": {
                        "byteStart": u["start"],
                        "byteEnd": u["end"],
                    },
                    "features": [
                        {
                            "$type": "app.bsky.richtext.facet#link",
                            "uri": u["url"],
                        }
                    ],
                }
            )
        for t in BlueSky._parse_tags(text):
            f.append(
                {
                    "index": {
                        "byteStart": t["start"],
                        "byteEnd": t["end"],
                    },
                    "features": [
                        {
                            "$type": "app.bsky.richtext.facet#tag",
                            "tag": t["tag"],
                        }
                    ],
                }
            )
        return f

    def post(self, text, media=None):
        e = list()
        if isinstance(media, list) and len(media) > 0:
            for i in media:
                if not isinstance(i, str):
                    raise ValueError("media list must only contain string values")
                if len(i) == 0:
                    continue
                e.append(self._make_blob(i))
        p = {
            "$type": "app.bsky.feed.post",
            "text": text,
            "langs": ["en-US"],
            "facets": self._prase_facets(text),
            "createdAt": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
        }
        if len(e) > 0:
            v = list()
            for i in e:
                v.append({"alt": "", "image": i})
            p["embed"] = {
                "$type": "app.bsky.embed.images",
                "images": v,
            }
            del v
        del e
        r = self._req.post(
            "https://bsky.social/xrpc/com.atproto.repo.createRecord",
            headers={"Authorization": f"Bearer {self._jwt}"},
            json={
                "repo": self._did,
                "record": p,
                "collection": "app.bsky.feed.post",
            },
        )
        try:
            j = r.json()
        finally:
            r.close()
            del r
        if "error" in j:
            raise ValueError(j["error"])
        elif "cid" not in j:
            raise ValueError(
                "xrpc/com.atproto.repo.createRecord: returned an invalid response"
            )
        return j["cid"]

    def _authenticate(self, user, password):
        r = self._req.post(
            "https://bsky.social/xrpc/com.atproto.server.createSession",
            json={
                "identifier": user,
                "password": password,
            },
        )
        try:
            j = r.json()
        finally:
            r.close()
            del r
        if "error" in j:
            raise ValueError(j["error"])
        elif "accessJwt" not in j or "did" not in j:
            raise ValueError(
                "xrpc/com.atproto.server.createSession: returned an invalid response"
            )
        self._did, self._jwt = j["did"], j["accessJwt"]
        del j


class CrossPoster(StreamListener):
    __slots__ = (
        "uid",
        "name",
        "handle",
        "prefix",
        "replace",
        "twitter",
        "bluesky",
        "mastodon",
    )

    def __init__(self, twitter, bluesky, mastodon, prefix=None, replace=None):
        StreamListener.__init__(self)
        self.handle = None
        self.prefix = prefix
        self.bluesky = bluesky
        self.twitter = twitter
        self.replace = replace
        self.mastodon = mastodon
        a = mastodon.account_verify_credentials()
        self.uid, self.name = a["id"], a["acct"]
        del a

    def close(self):
        if self.handle is None:
            return
        self.handle.close()
        self.handle = None

    def start(self):
        self.handle = self.mastodon.stream_user(
            self, run_async=True, reconnect_async=False, timeout=60
        )

    def on_update(self, x):
        if self.twitter is None and self.bluesky is None:
            return
        if x["account"]["id"] != self.uid:
            return
        if x["visibility"] != "public" or ("reblogged" in x and x["reblogged"]):
            return
        if x["in_reply_to_id"] is not None or x["in_reply_to_account_id"] is not None:
            return
        if "reblog" in x and x["reblog"] is not None:
            return
        if "content" not in x or len(x["content"]) == 0:
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
        ).text
        if isinstance(self.replace, dict):
            for k, v in self.replace.items():
                if not isinstance(k, str) or len(k) == 0:
                    continue
                if not isinstance(v, str) or len(v) == 0:
                    continue
                c = c.replace(k, v)
        if self.twitter is not None:
            y = c.replace("@twitter.com", "")
            if len(y) > 280:
                y = y[0:276] + " ..."
            if isinstance(self.prefix, str) and len(self.prefix) > 0:
                v = self.prefix + "/" + str(x["id"])
                if len(y) + len(v) + 1 <= 280:
                    y = y + "\n" + v
                del v
            try:
                t = self.twitter.post(y, m)
            except Exception as err:
                print(f"[{self.name}] Could not post Tweet: {err}!", file=stderr)
            else:
                print(f'[{self.name}] Posted Tweet "{t}"!')
            del y
        if self.bluesky is not None:
            y = c.replace("@twitter.com", "")
            if len(y) > 300:
                y = y[0:290] + " ..."
            if isinstance(self.prefix, str) and len(self.prefix) > 0:
                v = self.prefix + "/" + str(x["id"])
                if len(y) + len(v) + 1 <= 300:
                    y = y + "\n" + v
                del v
            try:
                t = self.bluesky.post(y, m)
            except Exception as err:
                print(f"[{self.name}] Could not post Skeet: {err}!", file=stderr)
            else:
                print(f'[{self.name}] Posted Skeet "{t}"!')
        del c
        if not isinstance(m, list) or len(m) == 0:
            return
        for i in m:
            try:
                remove(i)
            except OSError as err:
                print(
                    f'[{self.name}] Could not delete temp file "{i}": {err}!',
                    file=stderr,
                )
        del m


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
    threading.excepthook = _except_hook
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
