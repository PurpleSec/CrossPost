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

package main

import (
	"flag"
	"os"

	crosspost "github.com/PurpleSec/CrossPost"
)

var buildVersion = "unknown"

const version = "v3.0.0"

const usage = `CrossPost ` + version + `: forward Mastodon Posts to Twitter (and BlueSky!)
Purple Security (losynth.com/purple) 2021 - 2025

Usage:
  -h         Print this help menu.
  -V         Print version string and exit.
  -f <file>  Configuration file path.
  -d         Dump the default configuration and exit.

The only optional values are the "prefix" and "replace" values.

Prefix which takes a URL value that will be appended to Tweets (if the char limit
allows!) with the Mastodon post ID. This can be used as a quasi-link shortener.

Replace will replace the specified string matching phrases with the specified string
or character (or emoji!). These are case sensitive.
`

func main() {
	var (
		args      = flag.NewFlagSet("CrossPost "+version+"_"+buildVersion, flag.ExitOnError)
		file      string
		dump, ver bool
	)
	args.Usage = func() {
		os.Stderr.WriteString(usage)
		os.Exit(2)
	}
	args.StringVar(&file, "f", "", "")
	args.BoolVar(&dump, "d", false, "")
	args.BoolVar(&ver, "V", false, "")

	if err := args.Parse(os.Args[1:]); err != nil {
		os.Stderr.WriteString(usage)
		os.Exit(2)
	}

	if ver {
		os.Stdout.WriteString("CrossPost: " + version + "_" + buildVersion + "\n")
		os.Exit(0)
	}

	if len(file) == 0 && !dump {
		os.Stderr.WriteString(usage)
		os.Exit(2)
	}

	if dump {
		os.Stdout.WriteString(crosspost.Defaults)
		os.Exit(0)
	}

	s, err := crosspost.New(file)
	if err != nil {
		os.Stdout.WriteString("Error: " + err.Error() + "!\n")
		os.Exit(1)
	}

	if err := s.Run(); err != nil {
		os.Stdout.WriteString("Error: " + err.Error() + "!\n")
		os.Exit(1)
	}
}
