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
	"encoding/json"
	"errors"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"

	"github.com/PurpleSec/logx"
)

// CrossPost is a struct that contains the threads and config values that can be
// used to run the CrossPost service.
//
// Use the 'New' function to properly create a CrossPost service struct.
type CrossPost struct {
	_        [0]func()
	log      logx.Log
	cancel   context.CancelFunc
	accounts []*postAccount
}

// Run will start the main CrossPost service and all associated threads. This
// function will block until an interrupt signal is received.
//
// This function returns any errors that occur during shutdown.
func (c *CrossPost) Run() error {
	var (
		o   = make(chan os.Signal, 1)
		x   context.Context
		g   sync.WaitGroup
		err error
	)
	signal.Notify(o, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	x, c.cancel = context.WithCancel(context.Background())
	c.log.Info("CrossPost Started, spinning up sender/receiver threads..")
	for i := range c.accounts {
		c.log.Debug(`[%s]: Starting stream monitor "%s"..`, c.accounts[i].name, c.accounts[i].name)
		if err = c.accounts[i].start(x, &g); err != nil {
			c.log.Debug(`[%s]: Stream monitor "%s" start failed: %s!`, c.accounts[i].name, c.accounts[i].name, err.Error())
			goto cleanup
		}
	}
	for {
		select {
		case <-o:
			goto cleanup
		case <-x.Done():
			goto cleanup
		}
	}
cleanup:
	if signal.Stop(o); x.Err() != nil {
		// Propagate any errors from the listener.
		err = errors.New(`connection closed`)
	}
	c.cancel()
	g.Wait()
	close(o)
	return err
}

// New returns a new CrossPost instance based on the passed config file path. This
// function will preform any setup steps needed to start the CrossPost service.
// Once complete, use the 'Run' function to actually start the service.
func New(s string) (*CrossPost, error) {
	var c config
	j, err := os.ReadFile(s)
	if err != nil {
		return nil, errors.New(`reading config "` + s + `" failed: ` + err.Error())
	}
	if err = json.Unmarshal(j, &c); err != nil {
		return nil, errors.New(`parsing config "` + s + `" failed: ` + err.Error())
	}
	if err = c.check(); err != nil {
		return nil, err
	}
	l := logx.Multiple(logx.Console(logx.Level(c.Log.Level)))
	if len(c.Log.File) > 0 {
		f, err2 := logx.File(c.Log.File, logx.Append, logx.Level(c.Log.Level))
		if err2 != nil {
			return nil, errors.New(`log file "` + c.Log.File + `" creation failed: ` + err2.Error())
		}
		l.Add(f)
	}
	x := &CrossPost{log: l, accounts: make([]*postAccount, 0, len(c.Accounts))}
	for i, v := range c.Accounts {
		if err = x.newPostAccount(context.Background(), &v, c.Timeout); err != nil {
			return nil, errors.New(`account "` + strconv.Itoa(i) + `" setup failed: ` + err.Error())
		}
	}
	return x, nil
}
