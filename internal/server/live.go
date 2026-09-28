package server

// One live session (Jus contract 18), as in Jusplay: the page reports what
// it shows (POST api/state); events.watch streams it, the changes made to
// the notebook (the command line tells the window, events.notify) and the
// commands sent to the page, each with who did it. A command (editor open)
// waits until the page says it is done.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Tieumi221E/Jus/capreg"
)

type live struct {
	mu      sync.Mutex
	state   map[string]any
	last    string
	subs    map[*liveSub]bool
	waiters map[string]chan liveAck
}

type liveSub struct {
	ch   chan []byte
	page bool
}

type liveAck struct {
	state  map[string]any
	result any
	err    string
}

// Event is one line of events.watch.
type Event struct {
	Kind   string         `json:"kind"` // hello, state, changed, command, tick
	Time   time.Time      `json:"time"`
	Source capreg.Source  `json:"source"`
	Data   map[string]any `json:"data,omitempty"`
}

func (l *live) init() {
	if l.subs == nil {
		l.subs = map[*liveSub]bool{}
		l.waiters = map[string]chan liveAck{}
	}
}

func (s *Server) publish(kind string, src capreg.Source, data map[string]any) {
	b, err := json.Marshal(Event{Kind: kind, Time: time.Now(), Source: src, Data: data})
	if err != nil {
		return
	}
	s.live.mu.Lock()
	defer s.live.mu.Unlock()
	s.live.init()
	for sub := range s.live.subs {
		select {
		case sub.ch <- b:
		default:
		}
	}
}

func (s *Server) liveState() map[string]any {
	s.live.mu.Lock()
	defer s.live.mu.Unlock()
	out := map[string]any{}
	for k, v := range s.live.state {
		out[k] = v
	}
	return out
}

func (s *Server) pagesListening() int {
	s.live.mu.Lock()
	defer s.live.mu.Unlock()
	n := 0
	for sub := range s.live.subs {
		if sub.page {
			n++
		}
	}
	return n
}

func (s *Server) watch(ctx context.Context, emit func(any) error) error {
	sub := &liveSub{ch: make(chan []byte, 256), page: capreg.SourceOf(ctx).Via == "window"}
	s.live.mu.Lock()
	s.live.init()
	s.live.subs[sub] = true
	s.live.mu.Unlock()
	defer func() {
		s.live.mu.Lock()
		delete(s.live.subs, sub)
		s.live.mu.Unlock()
	}()
	if err := emit(Event{Kind: "hello", Time: time.Now(), Source: capreg.Source{Via: "window"}, Data: map[string]any{"state": s.liveState()}}); err != nil {
		return err
	}
	keep := time.NewTicker(20 * time.Second)
	defer keep.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case b := <-sub.ch:
			if err := emit(json.RawMessage(b)); err != nil {
				return err
			}
		case <-keep.C:
			if err := emit(Event{Kind: "tick", Time: time.Now(), Source: capreg.Source{Via: "window"}}); err != nil {
				return err
			}
		}
	}
}

// handleState is POST api/state, from the page.
func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	var req struct {
		State  map[string]any `json:"state"`
		Ack    string         `json:"ack"`
		Result any            `json:"result"`
		Error  string         `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.State != nil {
		same, _ := json.Marshal(req.State)
		req.State["updated"] = time.Now()
		s.live.mu.Lock()
		old := s.live.last
		s.live.state, s.live.last = req.State, string(same)
		s.live.mu.Unlock()
		if old != string(same) {
			s.publish("state", capreg.Source{Via: "window"}, req.State)
		}
	}
	if req.Ack != "" {
		s.live.mu.Lock()
		s.live.init()
		ch := s.live.waiters[req.Ack]
		delete(s.live.waiters, req.Ack)
		s.live.mu.Unlock()
		if ch != nil {
			ch <- liveAck{state: s.liveState(), result: req.Result, err: req.Error}
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// Command sends cmd to the page and waits until it is carried out.
func (s *Server) Command(ctx context.Context, cmd string, args map[string]any, wait time.Duration) (any, error) {
	if s.pagesListening() == 0 {
		return nil, errors.New("no window page is open to carry it out")
	}
	var b [8]byte
	rand.Read(b[:])
	id := hex.EncodeToString(b[:])
	ch := make(chan liveAck, 1)
	s.live.mu.Lock()
	s.live.init()
	s.live.waiters[id] = ch
	s.live.mu.Unlock()
	defer func() {
		s.live.mu.Lock()
		delete(s.live.waiters, id)
		s.live.mu.Unlock()
	}()
	data := map[string]any{"id": id, "cmd": cmd}
	for k, v := range args {
		data[k] = v
	}
	s.publish("command", capreg.SourceOf(ctx), data)
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case a := <-ch:
		if a.err != "" {
			return nil, errors.New(a.err)
		}
		if a.result != nil {
			return a.result, nil
		}
		return a.state, nil
	case <-t.C:
		return nil, errors.New("the window did not carry it out in time")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// RegisterWindow adds the window's own capabilities: they run in the open
// window (the command line hands them over). get is the window's server
// (nil on the command line).
func RegisterWindow(r *capreg.Registry, get func() *Server) {
	with := func(run func(ctx context.Context, s *Server, a capreg.Args) (any, error)) func(context.Context, capreg.Args) (any, error) {
		return func(ctx context.Context, a capreg.Args) (any, error) {
			if get == nil || get() == nil {
				return nil, errors.New("this needs the Jusnote window")
			}
			return run(ctx, get(), a)
		}
	}
	r.Add(capreg.Cap{ID: "events.watch", Summary: "what happens in the window, as it happens (one JSON object per line): the note shown, changes to the notebook and who made them, commands", Window: true,
		Stream: func(ctx context.Context, a capreg.Args, emit func(any) error) error {
			if get == nil || get() == nil {
				return errors.New("this needs the Jusnote window")
			}
			return get().watch(ctx, emit)
		}})

	r.Add(capreg.Cap{ID: "events.notify", Summary: "tell the window that its notebook changed (the command line does it after each write; after editing files with your own tools, say so too)", Window: true,
		Params: []capreg.Param{
			{Name: "notebook", Kind: capreg.Path, Doc: "the notebook folder that changed (default: the window's)"},
			{Name: "cap", Kind: capreg.String, Doc: "what changed it (a command's name)"},
			{Name: "paths", Kind: capreg.Strings, Doc: "the files changed, when known"}},
		Run: with(func(ctx context.Context, s *Server, a capreg.Args) (any, error) {
			svc := s.current()
			if svc == nil {
				return map[string]any{"shown": false}, nil
			}
			if nb := a.String("notebook"); nb != "" && !strings.EqualFold(filepath.Clean(nb), filepath.Clean(svc.NB.Root())) {
				return map[string]any{"shown": false, "why": "the window has another notebook open"}, nil
			}
			s.publish("changed", capreg.SourceOf(ctx), map[string]any{"cap": a.String("cap"), "paths": a.Strings("paths")})
			return map[string]any{"shown": true}, nil
		})})

	r.Add(capreg.Cap{ID: "editor.status", Summary: "what the window shows now: the notebook, the note, the line, unsaved changes", Window: true,
		Run: with(func(ctx context.Context, s *Server, a capreg.Args) (any, error) {
			return map[string]any{"state": s.liveState(), "pages": s.pagesListening()}, nil
		})})

	r.Add(capreg.Cap{ID: "notebook.recent", Summary: "the notebooks opened lately, newest first (the welcome screen's list)", Window: true,
		Run: with(func(ctx context.Context, s *Server, a capreg.Args) (any, error) {
			return s.recent.all(), nil
		})})

	r.Add(capreg.Cap{ID: "prefs.get", Summary: "the editor's settings (language, theme, font, size, wrap, guides, numbers, files)", Window: true,
		Run: with(func(ctx context.Context, s *Server, a capreg.Args) (any, error) {
			return s.prefs.get(), nil
		})})

	r.Add(capreg.Cap{ID: "prefs.set", Summary: "change one of the editor's settings; the window shows it at once", Window: true, Writes: true,
		Params: []capreg.Param{
			{Name: "key", Kind: capreg.String, Required: true, Positional: true, Enum: []string{"lang", "theme", "font", "size", "wrap", "guides", "numbers", "files"}},
			{Name: "value", Kind: capreg.String, Required: true, Positional: true, Doc: "zh|ja, dark|light, sans|mono, a size in px, true|false, all|notes"}},
		Run: with(func(ctx context.Context, s *Server, a capreg.Args) (any, error) {
			k, v := a.String("key"), a.String("value")
			raw := json.RawMessage(v) // size, wrap, guides, numbers: a number or true|false
			if k == "lang" || k == "theme" || k == "font" || k == "files" || !json.Valid(raw) {
				raw, _ = json.Marshal(v)
			}
			b, _ := json.Marshal(map[string]json.RawMessage{k: raw})
			p, err := s.prefs.update(b)
			if err != nil {
				return nil, capreg.Usagef("%v", err)
			}
			if s.pagesListening() > 0 {
				var val any
				json.Unmarshal(raw, &val)
				if _, err := s.Command(ctx, "pref", map[string]any{"key": k, "value": val}, 5*time.Second); err != nil {
					return nil, err
				}
			}
			return p, nil
		})})

	r.Add(capreg.Cap{ID: "notebook.open", Summary: "open a notebook (folder) in the window, as the window's Open does (it becomes a git repository if it is not one)", Window: true,
		Params: []capreg.Param{{Name: "folder", Kind: capreg.Path, Required: true, Positional: true, Doc: "the notebook folder"}},
		Run: with(func(ctx context.Context, s *Server, a capreg.Args) (any, error) {
			folder := filepath.Clean(a.String("folder"))
			if svc := s.current(); svc != nil && strings.EqualFold(folder, filepath.Clean(svc.NB.Root())) {
				return map[string]any{"root": svc.NB.Root(), "switched": false}, nil
			}
			// The page leaves its note first (saving it), then opens the folder.
			if _, err := s.Command(ctx, "notebook", map[string]any{"folder": folder}, 20*time.Second); err != nil {
				return nil, err
			}
			svc := s.current()
			if svc == nil || !strings.EqualFold(folder, filepath.Clean(svc.NB.Root())) {
				return nil, errors.New("the window did not open " + folder + " (unsaved changes kept?)")
			}
			return map[string]any{"root": svc.NB.Root(), "switched": true}, nil
		})})

	r.Add(capreg.Cap{ID: "editor.open", Summary: "show a note in the window (at -line)", Window: true,
		Params: []capreg.Param{{Name: "path", Kind: capreg.String, Required: true, Positional: true, Doc: "the note (relative to the window's notebook)"},
			{Name: "line", Kind: capreg.Int, Doc: "the line to show (1 is the first)"}},
		Run: with(func(ctx context.Context, s *Server, a capreg.Args) (any, error) {
			svc := s.current()
			if svc == nil {
				return nil, errNoNotebook
			}
			rel, err := svc.Clean(a.String("path"))
			if err != nil {
				return nil, capreg.Usagef("%v", err)
			}
			if !svc.NB.Exists(rel) {
				return nil, capreg.NotFoundf("no note %s in %s", rel, svc.NB.Root())
			}
			args := map[string]any{"path": rel}
			if a.Has("line") {
				args["line"] = a.Int("line")
			}
			return s.Command(ctx, "open", args, 10*time.Second)
		})})
}
