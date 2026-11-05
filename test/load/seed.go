package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/LukasHirt/groupkit/store"
	"github.com/LukasHirt/groupkit/store/filestore"

	"github.com/owncloud/ocis-groupware/internal/principals"
	"github.com/owncloud/ocis-groupware/internal/sharing"
)

// user is a seeded principal and the app password to sign in with.
type user struct {
	Name   string
	Owner  string
	Secret string
}

// largeCalendar is a calendar of one owner with Size events.
type largeCalendar struct {
	Size  int
	User  user
	Path  string
	Total int
}

// seeded is what seed wrote.
type seeded struct {
	Users []user
	Large []largeCalendar
	// SharedPaths maps a recipient's username to the paths of the calendars
	// shared with it.
	SharedPaths map[string][]string
	// LargeShared maps a recipient of the first large calendar to the path
	// the calendar has in its home.
	LargeShared map[string]string
	Objects     int
}

func userName(i int) string { return fmt.Sprintf("user%04d", i) }

// seed fills dir the way the service would have, through the packages the
// service is built from, while no service runs on dir. It opens the store
// and the registry itself, so the files are the real ones.
func seed(ctx context.Context, dir string, cfg config, progress func(string)) (*seeded, error) {
	logger := slog.New(slog.DiscardHandler)
	st, err := filestore.Open(dir, filestore.WithLogger(logger))
	if err != nil {
		return nil, err
	}
	defer func() { _ = st.Close() }()
	reg, err := principals.NewRegistry(dir, st, st.OwnerDir, logger, principals.WithBcryptCost(cfg.BcryptCost))
	if err != nil {
		return nil, err
	}

	out := &seeded{SharedPaths: map[string][]string{}, LargeShared: map[string]string{}}
	add := func(n int) { out.Objects += n }

	// Principals, each with an app password and an identity, which shares need.
	progress(fmt.Sprintf("seeding %d principals", cfg.Principals))
	total := cfg.Principals + len(cfg.LargeSizes)
	users := make([]user, total)
	err = parallel(ctx, total, 8, func(i int) error {
		name := userName(i)
		if i >= cfg.Principals {
			name = fmt.Sprintf("large%d", cfg.LargeSizes[i-cfg.Principals])
		}
		owner := "sub-" + name
		if _, _, err := reg.Provision(ctx, owner, name, name); err != nil {
			return err
		}
		if err := reg.SetIdentity(ctx, owner, principals.Identity{OCISID: "id-" + name, At: time.Now()}); err != nil {
			return err
		}
		_, secret, err := reg.IssueAppPassword(ctx, owner, "load", 0)
		if err != nil {
			return err
		}
		users[i] = user{Name: name, Owner: owner, Secret: secret}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out.Users = users[:cfg.Principals]

	// Each principal's own calendar.
	progress(fmt.Sprintf("seeding %d events per principal", cfg.EventsPerUser))
	err = parallel(ctx, cfg.Principals, 8, func(i int) error {
		return fill(ctx, st, calRef(out.Users[i].Owner, "personal"), out.Users[i].Name, cfg.EventsPerUser)
	})
	if err != nil {
		return nil, err
	}
	add(cfg.Principals * cfg.EventsPerUser)

	shares, err := sharing.Open(sharing.Config{
		OwnerDir: st.OwnerDir,
		Owners: func() []string {
			var owners []string
			for _, p := range reg.ListPrincipals(ctx) {
				owners = append(owners, p.Owner)
			}
			return owners
		},
	})
	if err != nil {
		return nil, err
	}

	// Large calendars. The first one is shared with the first recipients.
	for k, size := range cfg.LargeSizes {
		u := users[cfg.Principals+k]
		progress(fmt.Sprintf("seeding a calendar of %d events", size))
		if err := fill(ctx, st, calRef(u.Owner, "personal"), u.Name, size); err != nil {
			return nil, err
		}
		add(size)
		out.Large = append(out.Large, largeCalendar{Size: size, User: u, Path: "/dav/calendars/" + u.Name + "/personal/", Total: size})
		if k == 0 {
			paths, err := shareWith(ctx, st, shares, out, calRef(u.Owner, "personal"), cfg.RecipientsPer, 0)
			if err != nil {
				return nil, err
			}
			out.LargeShared = paths
		}
	}

	// Calendars shared with many users.
	progress(fmt.Sprintf("seeding %d shared calendars with %d recipients each", cfg.SharedCollections, cfg.RecipientsPer))
	for i := 0; i < cfg.SharedCollections; i++ {
		owner := out.Users[i]
		ref := calRef(owner.Owner, "team")
		if _, err := st.CreateCollection(ctx, ref, store.Props{DisplayName: "Team"}); err != nil {
			return nil, err
		}
		if err := fill(ctx, st, ref, owner.Name+"-team", cfg.SharedEvents); err != nil {
			return nil, err
		}
		add(cfg.SharedEvents)
		// Recipients start after the first block, so a client is rarely a
		// recipient of its own calendar's share.
		if _, err := shareWith(ctx, st, shares, out, ref, cfg.RecipientsPer, cfg.RecipientsPer+i*7); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// shareWith grants read access on ref to n users, starting at user index
// from. The recipients' DAV paths are recorded in out and returned by user.
func shareWith(ctx context.Context, st *filestore.Store, shares *sharing.Shares, out *seeded, ref store.CollectionRef, n, from int) (map[string]string, error) {
	coll, err := st.GetCollection(ctx, ref)
	if err != nil {
		return nil, err
	}
	paths := map[string]string{}
	granted := 0
	for j := 0; granted < n; j++ {
		idx := (from + j) % len(out.Users)
		u := out.Users[idx]
		if u.Owner == ref.Owner {
			continue
		}
		err := shares.Grant(ctx, ref, coll.ID, sharing.Member{Type: sharing.User, ID: "id-" + u.Name, Name: u.Name, Access: sharing.Read})
		if err != nil {
			return nil, err
		}
		p := "/dav/calendars/" + u.Name + "/" + sharing.SharedPrefix + coll.ID + "/"
		out.SharedPaths[u.Name] = append(out.SharedPaths[u.Name], p)
		paths[u.Name] = p
		granted++
	}
	return paths, nil
}

func calRef(owner, name string) store.CollectionRef {
	return store.CollectionRef{Owner: owner, Kind: store.KindCalendar, Name: name}
}

// fill writes n events into the calendar ref, creating it when it is missing.
func fill(ctx context.Context, st *filestore.Store, ref store.CollectionRef, prefix string, n int) error {
	if _, err := st.GetCollection(ctx, ref); err != nil {
		if _, err := st.CreateCollection(ctx, ref, store.Props{DisplayName: ref.Name}); err != nil {
			return err
		}
	}
	workers := 1
	if n >= 1000 {
		workers = 8
	}
	return parallel(ctx, n, workers, func(i int) error {
		_, err := st.PutObject(ctx, ref, fmt.Sprintf("%s-%d.ics", prefix, i), buildEvent(prefix, i), store.PutOptions{})
		return err
	})
}

// parallel runs fn for 0..n-1 on up to workers goroutines and returns the
// first error.
func parallel(ctx context.Context, n, workers int, fn func(i int) error) error {
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		first error
		next  int
	)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				mu.Lock()
				i := next
				next++
				stop := first != nil || i >= n || ctx.Err() != nil
				mu.Unlock()
				if stop {
					return
				}
				if err := fn(i); err != nil {
					mu.Lock()
					if first == nil {
						first = err
					}
					mu.Unlock()
					return
				}
			}
		}()
	}
	wg.Wait()
	if first == nil {
		first = ctx.Err()
	}
	return first
}
