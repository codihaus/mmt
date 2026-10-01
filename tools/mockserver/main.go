// Command mockserver is a tiny fake Mattermost server for trying mmt and
// working on the UI without a real server. It keeps everything in memory and
// implements only the endpoints mmt calls.
//
//	go run ./tools/mockserver            # listens on 127.0.0.1:8065
//	MMT_URL=http://127.0.0.1:8065 MMT_TOKEN=demo mmt --here
//
// Any token is accepted. Send a message containing "ping" to get a reply.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type obj = map[string]any

type server struct {
	mu       sync.Mutex
	seq      int
	users    map[string]obj
	channels []obj
	members  []obj
	posts    map[string][]obj
	files    map[string][]byte
	bots     []obj
	tokens   []obj
	conns    []*websocket.Conn

	teams    []obj
	cats     map[string]obj // team id -> sidebar categories
	joinable []obj          // public channels offered by search
	video    bool           // the scripted scenario with key controls
	current  string         // channel the client last viewed
	turn     int            // rotates who answers in the video scenario
}

const me = "u-demo"

func main() {
	addr := flag.String("addr", "127.0.0.1:8065", "listen address")
	scenario := flag.String("scenario", "demo", "demo (English sample data) or video (scripted scenario with key controls)")
	flag.Parse()
	var s *server
	if *scenario == "video" {
		s = newVideoServer()
		go s.controls()
	} else {
		s = newServer()
	}
	log.Printf("mock Mattermost on http://%s (any token works)", *addr)
	log.Fatal(http.ListenAndServe(*addr, s.routes()))
}

func newServer() *server {
	now := time.Now().UnixMilli()
	s := &server{
		users: map[string]obj{
			me:        {"id": me, "username": "demo", "first_name": "Demo", "last_name": "User"},
			"u-alice": {"id": "u-alice", "username": "alice", "first_name": "Alice", "last_name": "Nguyen"},
			"u-bob":   {"id": "u-bob", "username": "bob", "first_name": "Bob", "last_name": "Tran"},
			"u-carol": {"id": "u-carol", "username": "carol", "first_name": "Carol", "last_name": "Le"},
		},
		posts: map[string][]obj{},
		files: map[string][]byte{"f-chart": samplePNG()},
	}
	s.channels = []obj{
		{"id": "c-town", "team_id": "t-acme", "type": "O", "name": "town-square", "display_name": "Town Square", "header": "Company-wide announcements", "total_msg_count": 8, "last_post_at": now},
		{"id": "c-dev", "team_id": "t-acme", "type": "O", "name": "dev", "display_name": "dev", "total_msg_count": 3, "last_post_at": now},
		{"id": "c-release", "team_id": "t-acme", "type": "P", "name": "release", "display_name": "Release Planning", "total_msg_count": 1, "last_post_at": now},
		{"id": "c-random", "team_id": "t-acme", "type": "O", "name": "random", "display_name": "random", "total_msg_count": 9, "last_post_at": now},
		{"id": "c-ops", "team_id": "t-labs", "type": "O", "name": "ops", "display_name": "ops", "total_msg_count": 2, "last_post_at": now},
		{"id": "c-labs-town", "team_id": "t-labs", "type": "O", "name": "town-square", "display_name": "Town Square", "total_msg_count": 1, "last_post_at": now},
		{"id": "c-dm-alice", "type": "D", "name": me + "__u-alice", "total_msg_count": 1, "last_post_at": now},
		{"id": "c-dm-bob", "type": "D", "name": me + "__u-bob", "total_msg_count": 0, "last_post_at": now - 86400000},
	}
	s.members = []obj{
		{"channel_id": "c-town", "user_id": me, "msg_count": 8},
		{"channel_id": "c-dev", "user_id": me, "msg_count": 1, "mention_count": 1},
		{"channel_id": "c-release", "user_id": me, "msg_count": 1},
		{"channel_id": "c-random", "user_id": me, "msg_count": 2, "notify_props": obj{"mark_unread": "mention"}},
		{"channel_id": "c-ops", "user_id": me, "msg_count": 0},
		{"channel_id": "c-labs-town", "user_id": me, "msg_count": 1},
		{"channel_id": "c-dm-alice", "user_id": me, "msg_count": 0},
		{"channel_id": "c-dm-bob", "user_id": me, "msg_count": 0},
	}

	h := time.Hour
	s.add("c-town", "u-alice", "Morning all, we ship **v2.4** today :rocket:", 26*h, "")
	s.add("c-town", "u-bob", "ok", 25*h, "")
	root := s.add("c-town", "u-alice", "@demo could you review the migration PR? It touches the billing tables.", 3*h, "")
	s.add("c-town", me, "on it", 2*h, root["id"].(string))
	s.add("c-town", "u-alice", "thanks!", 2*h-time.Minute, root["id"].(string))
	root["reply_count"] = 2
	s.add("c-town", "u-carol", "Seeing this in staging:\n```\npanic: runtime error: index out of range [3] with length 3\n```", 40*time.Minute, "")
	s.add("c-town", "u-carol", "digging in", 39*time.Minute, "")
	chart := s.add("c-town", "u-bob", "Latency after the fix, details at https://example.com/dashboards/42", 30*time.Minute, "")
	chart["file_ids"] = []string{"f-chart"}
	chart["metadata"] = obj{
		"files": []obj{{"id": "f-chart", "name": "latency.png", "mime_type": "image/png", "width": 320, "height": 180}},
		"reactions": []obj{
			{"user_id": me, "post_id": chart["id"], "emoji_name": "+1", "create_at": 1},
			{"user_id": "u-carol", "post_id": chart["id"], "emoji_name": "+1", "create_at": 2},
			{"user_id": "u-carol", "post_id": chart["id"], "emoji_name": "eyes", "create_at": 3},
		},
	}
	standup := s.add("c-town", "u-carol", "carol started a call", 5*h, "")
	standup["type"] = "custom_calls"
	standup["props"] = obj{"start_at": time.Now().Add(-5 * h).UnixMilli(), "end_at": time.Now().Add(-5*h + 14*time.Minute).UnixMilli(),
		"title": "Standup", "participants": []string{"u-carol", "u-bob", me}}
	s.add("c-dev", "u-bob", "CI is green", 50*time.Minute, "")
	live := s.add("c-dev", "u-bob", "bob started a call", 5*time.Minute, "")
	live["type"] = "custom_calls"
	live["props"] = obj{"start_at": time.Now().Add(-5 * time.Minute).UnixMilli()}
	s.add("c-dev", "u-carol", "@demo ok to merge?", 10*time.Minute, "")
	s.add("c-dm-alice", "u-alice", "Are we still on for 3pm?", 5*time.Minute, "")

	s.teams = []obj{{"id": "t-acme", "name": "acme", "display_name": "Acme"}, {"id": "t-labs", "name": "labs", "display_name": "Acme Labs"}}
	s.cats = map[string]obj{
		"t-acme": {"order": []string{"fav", "proj", "ch", "dm"}, "categories": []obj{
			{"id": "fav", "type": "favorites", "display_name": "Favorites", "channel_ids": []string{"c-dev"}},
			{"id": "proj", "type": "custom", "display_name": "Projects", "sorting": "manual", "channel_ids": []string{"c-release", "c-town"}},
			{"id": "ch", "type": "channels", "display_name": "Channels", "sorting": "alpha", "collapsed": true, "channel_ids": []string{"c-random"}},
			{"id": "dm", "type": "direct_messages", "display_name": "Direct Messages", "channel_ids": []string{"c-dm-bob", "c-dm-alice"}},
		}},
		"t-labs": {"order": []string{"ch", "dm"}, "categories": []obj{
			{"id": "ch", "type": "channels", "display_name": "Channels", "sorting": "alpha", "channel_ids": []string{"c-ops", "c-labs-town"}},
			{"id": "dm", "type": "direct_messages", "display_name": "Direct Messages", "channel_ids": []string{"c-dm-alice", "c-dm-bob"}},
		}},
	}
	s.joinable = []obj{{"id": "c-alerts", "team_id": "t-acme", "type": "O", "name": "alerts", "display_name": "Alerts"}}
	return s
}

// add appends a post created `ago` before now. Callers hold no lock during
// setup; request handlers take s.mu first.
func (s *server) add(channel, user, msg string, ago time.Duration, root string) obj {
	s.seq++
	p := obj{"id": fmt.Sprintf("p%d", s.seq), "channel_id": channel, "user_id": user, "message": msg,
		"create_at": time.Now().Add(-ago).UnixMilli(), "root_id": root, "type": ""}
	s.posts[channel] = append(s.posts[channel], p)
	return p
}

// samplePNG draws a small bar chart so image features work offline.
func samplePNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 320, 180))
	bg := color.RGBA{0xf4, 0xf6, 0xfa, 0xff}
	bar := color.RGBA{0x3b, 0x82, 0xf6, 0xff}
	for y := 0; y < 180; y++ {
		for x := 0; x < 320; x++ {
			img.Set(x, y, bg)
		}
	}
	for i, h := range []int{120, 90, 140, 60, 40, 30} {
		for y := 170 - h; y < 170; y++ {
			for x := 20 + i*50; x < 55+i*50; x++ {
				img.Set(x, y, bar)
			}
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *server) broadcast(ev obj) {
	b, _ := json.Marshal(ev)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.conns {
		_ = c.WriteMessage(websocket.TextMessage, b)
	}
}

func (s *server) postedEvent(p obj, mentions []string) obj {
	raw, _ := json.Marshal(p)
	m, _ := json.Marshal(mentions)
	sender := s.users[p["user_id"].(string)]["username"]
	return obj{"event": "posted", "broadcast": obj{"channel_id": p["channel_id"]},
		"data": obj{"post": string(raw), "sender_name": "@" + sender.(string), "mentions": string(m)}}
}

func postList(list []obj) obj {
	order := make([]string, 0, len(list))
	byID := obj{}
	for i := len(list) - 1; i >= 0; i-- {
		id := list[i]["id"].(string)
		order = append(order, id)
		byID[id] = list[i]
	}
	return obj{"order": order, "posts": byID}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v4/users/me", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.users[me]) })
	// one route for /users/{id}/teams and /users/username/{name}: the two
	// patterns would otherwise overlap
	mux.HandleFunc("GET /api/v4/users/{a}/{b}", func(w http.ResponseWriter, r *http.Request) {
		switch r.PathValue("b") {
		case "status":
			writeJSON(w, obj{"user_id": r.PathValue("a"), "status": "online"})
			return
		case "tokens":
			s.mu.Lock()
			defer s.mu.Unlock()
			writeJSON(w, s.tokens)
			return
		}
		if r.PathValue("a") == "username" {
			for _, u := range s.users {
				if u["username"] == r.PathValue("b") {
					writeJSON(w, u)
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
			writeJSON(w, obj{"id": "not_found", "status_code": 404})
			return
		}
		writeJSON(w, s.teams)
	})
	mux.HandleFunc("GET /api/v4/users/{uid}/teams/{tid}/channels", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		out := []obj{}
		for _, c := range s.channels {
			if c["team_id"] == nil || c["team_id"] == r.PathValue("tid") {
				out = append(out, c)
			}
		}
		writeJSON(w, out)
	})
	mux.HandleFunc("GET /api/v4/users/{uid}/teams/{tid}/channels/members", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		writeJSON(w, s.members)
	})
	mux.HandleFunc("GET /api/v4/users/{uid}/teams/{tid}/channels/categories", func(w http.ResponseWriter, r *http.Request) {
		if c, ok := s.cats[r.PathValue("tid")]; ok {
			writeJSON(w, c)
			return
		}
		writeJSON(w, obj{"order": []string{}, "categories": []obj{}})
	})
	mux.HandleFunc("POST /api/v4/users/ids", func(w http.ResponseWriter, r *http.Request) {
		var ids []string
		_ = json.NewDecoder(r.Body).Decode(&ids)
		out := []obj{}
		for _, id := range ids {
			if u, ok := s.users[id]; ok {
				out = append(out, u)
			}
		}
		writeJSON(w, out)
	})
	mux.HandleFunc("GET /api/v4/users/autocomplete", func(w http.ResponseWriter, r *http.Request) {
		q := strings.ToLower(r.URL.Query().Get("name"))
		out := []obj{}
		for _, u := range s.users {
			if strings.HasPrefix(u["username"].(string), q) {
				out = append(out, u)
			}
		}
		writeJSON(w, obj{"users": out})
	})
	mux.HandleFunc("GET /api/v4/channels/{cid}/posts", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if r.URL.Query().Get("page") != "0" {
			writeJSON(w, obj{"order": []string{}, "posts": obj{}})
			return
		}
		writeJSON(w, postList(s.posts[r.PathValue("cid")]))
	})
	mux.HandleFunc("GET /api/v4/posts/{pid}/thread", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		pid := r.PathValue("pid")
		var list []obj
		for _, ps := range s.posts {
			for _, p := range ps {
				if p["id"] == pid || p["root_id"] == pid {
					list = append(list, p)
				}
			}
		}
		sort.Slice(list, func(i, j int) bool { return list[i]["create_at"].(int64) < list[j]["create_at"].(int64) })
		writeJSON(w, postList(list))
	})
	mux.HandleFunc("POST /api/v4/posts", s.createPost)
	mux.HandleFunc("POST /api/v4/channels", func(w http.ResponseWriter, r *http.Request) {
		var ch obj
		_ = json.NewDecoder(r.Body).Decode(&ch)
		s.mu.Lock()
		defer s.mu.Unlock()
		s.seq++
		ch["id"] = fmt.Sprintf("c-new%d", s.seq)
		ch["create_at"] = time.Now().UnixMilli()
		s.channels = append(s.channels, ch)
		s.members = append(s.members, obj{"channel_id": ch["id"], "user_id": me})
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, ch)
		if s.video {
			go s.welcome(ch["id"].(string))
		}
	})
	mux.HandleFunc("GET /api/v4/channels/{cid}", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, c := range s.channels {
			if c["id"] == r.PathValue("cid") {
				writeJSON(w, c)
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, obj{"id": "not_found", "status_code": 404})
	})
	mux.HandleFunc("PUT /api/v4/channels/{cid}/patch", func(w http.ResponseWriter, r *http.Request) {
		var patch obj
		_ = json.NewDecoder(r.Body).Decode(&patch)
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, c := range s.channels {
			if c["id"] == r.PathValue("cid") {
				if d, ok := patch["display_name"].(string); ok {
					c["display_name"] = d
				}
				writeJSON(w, c)
				return
			}
		}
	})
	mux.HandleFunc("DELETE /api/v4/channels/{cid}", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, obj{"status": "OK"}) })
	mux.HandleFunc("GET /api/v4/channels/{cid}/members", func(w http.ResponseWriter, r *http.Request) {
		out := []obj{}
		for id := range s.users {
			out = append(out, obj{"channel_id": r.PathValue("cid"), "user_id": id})
		}
		writeJSON(w, out)
	})
	mux.HandleFunc("DELETE /api/v4/channels/{cid}/members/{uid}", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, obj{"status": "OK"}) })
	mux.HandleFunc("POST /api/v4/channels/group", func(w http.ResponseWriter, r *http.Request) {
		var ids []string
		_ = json.NewDecoder(r.Body).Decode(&ids)
		var names []string
		for _, id := range ids {
			names = append(names, s.users[id]["username"].(string))
		}
		ch := obj{"id": "c-group", "type": "G", "name": "group-" + strings.Join(ids, "-"), "display_name": strings.Join(names, ", ")}
		s.mu.Lock()
		s.channels = append(s.channels, ch)
		s.mu.Unlock()
		writeJSON(w, ch)
	})
	mux.HandleFunc("POST /api/v4/teams/{tid}/members", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, obj{"team_id": r.PathValue("tid")})
	})
	mux.HandleFunc("GET /api/v4/bots", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		writeJSON(w, s.bots)
	})
	mux.HandleFunc("POST /api/v4/bots", func(w http.ResponseWriter, r *http.Request) {
		var b obj
		_ = json.NewDecoder(r.Body).Decode(&b)
		s.mu.Lock()
		defer s.mu.Unlock()
		id := "u-bot-" + b["username"].(string)
		b["user_id"], b["owner_id"] = id, me
		s.bots = append(s.bots, b)
		s.users[id] = obj{"id": id, "username": b["username"], "is_bot": true}
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, b)
	})
	mux.HandleFunc("POST /api/v4/users/{uid}/tokens", func(w http.ResponseWriter, r *http.Request) {
		var in obj
		_ = json.NewDecoder(r.Body).Decode(&in)
		s.mu.Lock()
		defer s.mu.Unlock()
		s.seq++
		t := obj{"id": fmt.Sprintf("tok%d", s.seq), "user_id": r.PathValue("uid"), "description": in["description"], "is_active": true}
		s.tokens = append(s.tokens, t)
		out := obj{"token": fmt.Sprintf("mock-token-%d", s.seq)}
		for k, v := range t {
			out[k] = v
		}
		writeJSON(w, out)
	})
	mux.HandleFunc("POST /api/v4/users/tokens/revoke", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, obj{"status": "OK"}) })
	mux.HandleFunc("POST /api/v4/channels/members/{uid}/view", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ChannelID string `json:"channel_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		s.mu.Lock()
		s.current = in.ChannelID
		s.mu.Unlock()
		writeJSON(w, obj{"status": "OK"})
	})
	mux.HandleFunc("GET /api/v4/teams/{tid}/channels/autocomplete", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, s.joinable)
	})
	mux.HandleFunc("POST /api/v4/channels/{cid}/members", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, obj{"channel_id": r.PathValue("cid"), "user_id": me})
	})
	mux.HandleFunc("POST /api/v4/files", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f, hdr, err := r.FormFile("files")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer f.Close()
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(f)
		s.mu.Lock()
		id := fmt.Sprintf("f%d", len(s.files)+1)
		s.files[id] = buf.Bytes()
		s.mu.Unlock()
		writeJSON(w, obj{"file_infos": []obj{{"id": id, "name": hdr.Filename, "size": hdr.Size}}})
	})
	serveFile := func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		data := s.files[r.PathValue("fid")]
		s.mu.Unlock()
		_, _ = w.Write(data)
	}
	mux.HandleFunc("GET /api/v4/files/{fid}", serveFile)
	mux.HandleFunc("GET /api/v4/files/{fid}/preview", serveFile)
	mux.HandleFunc("GET /api/v4/websocket", s.websocket)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Println("not implemented:", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, obj{"id": "not_found", "message": "not implemented by mockserver", "status_code": 404})
	})
	return mux
}

func (s *server) createPost(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ChannelID     string   `json:"channel_id"`
		RootID        string   `json:"root_id"`
		Message       string   `json:"message"`
		PendingPostID string   `json:"pending_post_id"`
		FileIDs       []string `json:"file_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	p := s.add(in.ChannelID, me, in.Message, 0, in.RootID)
	p["pending_post_id"] = in.PendingPostID
	if len(in.FileIDs) > 0 {
		var files []obj
		for _, id := range in.FileIDs {
			files = append(files, obj{"id": id, "name": id + ".png", "mime_type": "image/png"})
		}
		p["file_ids"] = in.FileIDs
		p["metadata"] = obj{"files": files}
	}
	ev := s.postedEvent(p, nil)
	s.mu.Unlock()
	s.broadcast(ev)
	writeJSON(w, p)

	if s.video {
		go s.respond(p)
		return
	}
	if strings.Contains(strings.ToLower(in.Message), "ping") {
		go func() {
			time.Sleep(time.Second)
			s.broadcast(obj{"event": "typing", "data": obj{"user_id": "u-alice", "parent_id": in.RootID}, "broadcast": obj{"channel_id": in.ChannelID}})
			time.Sleep(2 * time.Second)
			s.mu.Lock()
			q := s.add(in.ChannelID, "u-alice", "pong :wave:", 0, in.RootID)
			ev := s.postedEvent(q, nil)
			s.mu.Unlock()
			s.broadcast(ev)
		}()
	}
}

// websocket accepts the client and keeps the connection for broadcasts.
func (s *server) websocket(w http.ResponseWriter, r *http.Request) {
	c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
	if err != nil {
		return
	}
	// say hello before the connection joins broadcasts: one writer at a time
	_ = c.WriteJSON(obj{"event": "hello", "data": obj{}, "broadcast": obj{}})
	s.mu.Lock()
	s.conns = append(s.conns, c)
	s.mu.Unlock()

	// the video scenario is driven by the key controls instead
	if !s.video {
		go s.demoEvents()
	}

	for {
		if _, _, err := c.ReadMessage(); err != nil {
			s.mu.Lock()
			for i, x := range s.conns {
				if x == c {
					s.conns = append(s.conns[:i], s.conns[i+1:]...)
					break
				}
			}
			s.mu.Unlock()
			return
		}
	}
}

// demoEvents plays a reaction, an incoming call and a mention a few seconds
// after connecting, so live updates show up without a second client.
func (s *server) demoEvents() {
	func() {
		time.Sleep(2 * time.Second)
		s.mu.Lock()
		var target string
		for _, p := range s.posts["c-town"] {
			if p["file_ids"] != nil {
				target = p["id"].(string)
			}
		}
		s.mu.Unlock()
		raw, _ := json.Marshal(obj{"user_id": "u-alice", "post_id": target, "emoji_name": "white_check_mark", "create_at": 9})
		s.broadcast(obj{"event": "reaction_added", "data": obj{"reaction": string(raw)}, "broadcast": obj{"channel_id": "c-town"}})

		time.Sleep(time.Second)
		s.broadcast(obj{"event": "custom_com.mattermost.calls_call_start", "broadcast": obj{"channel_id": "c-dm-alice"},
			"data": obj{"id": "call-1", "channelID": "c-dm-alice", "owner_id": "u-alice", "start_at": time.Now().UnixMilli()}})

		time.Sleep(time.Second)
		s.mu.Lock()
		p := s.add("c-dev", "u-carol", "@demo the release branch is ready", 0, "")
		ev := s.postedEvent(p, []string{me})
		s.mu.Unlock()
		s.broadcast(ev)
	}()
}
