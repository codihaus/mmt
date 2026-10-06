package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

// The video scenario: the Straw Hat crew from One Piece, for screen
// recordings. Teammates answer whatever you post, and the key
// controls (run the server in a second terminal) trigger the moments a demo
// needs on cue: a mention, an incoming call, a DM while mmt is locked.

// The three crew members the scenario revolves around keep their old ids;
// zoro, sanji and usopp only chat in the galley.
const (
	uLan   = "u-lan"  // robin
	uKhoa  = "u-khoa" // nami
	uDuc   = "u-duc"  // franky
	uZoro  = "u-zoro"
	uSanji = "u-sanji"
	uUsopp = "u-usopp"
	uNews  = "u-news" // a bot that posts message attachments
)

func newVideoServer() *server {
	now := time.Now().UnixMilli()
	s := &server{
		video: true,
		users: map[string]obj{
			me:     {"id": me, "username": "luffy", "first_name": "Monkey D.", "last_name": "Luffy", "position": "Captain"},
			uLan:   {"id": uLan, "username": "robin", "first_name": "Nico", "last_name": "Robin", "position": "Archaeologist"},
			uKhoa:  {"id": uKhoa, "username": "nami", "first_name": "Nami", "position": "Navigator"},
			uDuc:   {"id": uDuc, "username": "franky", "first_name": "Franky", "position": "Shipwright"},
			uZoro:  {"id": uZoro, "username": "zoro", "first_name": "Roronoa", "last_name": "Zoro", "position": "Swordsman"},
			uSanji: {"id": uSanji, "username": "sanji", "first_name": "Sanji", "position": "Cook"},
			uUsopp: {"id": uUsopp, "username": "usopp", "first_name": "Usopp", "position": "Sniper"},
			uNews:  {"id": uNews, "username": "news_coo", "first_name": "News Coo", "is_bot": true},
		},
		posts: map[string][]obj{},
		files: map[string][]byte{"f-radar": radarPNG()},
	}
	s.teams = []obj{
		{"id": "t-404", "name": "straw-hats", "display_name": "Straw Hats"},
		{"id": "t-dem", "name": "thousand-sunny", "display_name": "Thousand Sunny"},
	}
	s.channels = []obj{
		{"id": "c-chithi", "team_id": "t-404", "type": "O", "name": "town-square", "display_name": "Captain's Orders", "header": "From the captain · read it, then set sail", "total_msg_count": 9, "last_post_at": now},
		{"id": "c-trinhsat", "team_id": "t-404", "type": "O", "name": "lookout", "display_name": "Lookout", "header": "Reports from the crow's nest", "total_msg_count": 3, "last_post_at": now},
		{"id": "c-lab", "team_id": "t-404", "type": "P", "name": "sick-bay", "display_name": "Sick Bay", "total_msg_count": 1, "last_post_at": now},
		{"id": "c-kho", "team_id": "t-404", "type": "P", "name": "treasure-vault", "display_name": "Treasure Vault", "total_msg_count": 1, "last_post_at": now},
		{"id": "c-tangau", "team_id": "t-404", "type": "O", "name": "galley", "display_name": "Galley", "total_msg_count": 6, "last_post_at": now},
		{"id": "c-dem-town", "team_id": "t-dem", "type": "O", "name": "town-square", "display_name": "Deck", "total_msg_count": 1, "last_post_at": now},
		{"id": "c-dem-ops", "team_id": "t-dem", "type": "O", "name": "engine-room", "display_name": "Engine Room", "total_msg_count": 2, "last_post_at": now},
		{"id": "c-dm-lan", "type": "D", "name": me + "__" + uLan, "total_msg_count": 1, "last_post_at": now},
		{"id": "c-dm-khoa", "type": "D", "name": me + "__" + uKhoa, "total_msg_count": 0, "last_post_at": now - 3600000},
		{"id": "c-dm-duc", "type": "D", "name": me + "__" + uDuc, "total_msg_count": 0, "last_post_at": now - 86400000},
	}
	s.members = []obj{
		{"channel_id": "c-chithi", "user_id": me, "msg_count": 9},
		{"channel_id": "c-trinhsat", "user_id": me, "msg_count": 1, "mention_count": 1},
		{"channel_id": "c-lab", "user_id": me, "msg_count": 0},
		{"channel_id": "c-kho", "user_id": me, "msg_count": 1},
		{"channel_id": "c-tangau", "user_id": me, "msg_count": 2, "notify_props": obj{"mark_unread": "mention"}},
		{"channel_id": "c-dem-town", "user_id": me, "msg_count": 1},
		{"channel_id": "c-dem-ops", "user_id": me, "msg_count": 0},
		{"channel_id": "c-dm-lan", "user_id": me, "msg_count": 0},
		{"channel_id": "c-dm-khoa", "user_id": me, "msg_count": 0},
		{"channel_id": "c-dm-duc", "user_id": me, "msg_count": 0},
	}
	s.cats = map[string]obj{
		"t-404": {"order": []string{"fav", "op", "ch", "dm"}, "categories": []obj{
			{"id": "fav", "type": "favorites", "display_name": "Priority", "channel_ids": []string{"c-trinhsat"}},
			{"id": "op", "type": "custom", "display_name": "Course to Elbaf", "sorting": "manual", "channel_ids": []string{"c-chithi", "c-lab", "c-kho"}},
			{"id": "ch", "type": "channels", "display_name": "Channels", "sorting": "alpha", "collapsed": true, "channel_ids": []string{"c-tangau"}},
			{"id": "dm", "type": "direct_messages", "display_name": "Direct Messages", "channel_ids": []string{"c-dm-lan", "c-dm-khoa", "c-dm-duc"}},
		}},
		"t-dem": {"order": []string{"ch", "dm"}, "categories": []obj{
			{"id": "ch", "type": "channels", "display_name": "Channels", "sorting": "alpha", "channel_ids": []string{"c-dem-town", "c-dem-ops"}},
			{"id": "dm", "type": "direct_messages", "display_name": "Direct Messages", "channel_ids": []string{"c-dm-lan", "c-dm-khoa", "c-dm-duc"}},
		}},
	}
	s.joinable = []obj{{"id": "c-dulieu", "team_id": "t-404", "type": "O", "name": "road-poneglyphs", "display_name": "Road Poneglyphs"}}

	h := time.Hour
	s.add("c-chithi", uKhoa, "We set sail at **06:00**. The Log Pose points north :ship:", 26*h, "")
	s.add("c-chithi", uLan, "understood", 25*h, "")
	brief := s.add("c-chithi", uKhoa, "nami started a call", 6*h, "")
	brief["type"] = "custom_calls"
	brief["props"] = obj{"start_at": time.Now().Add(-6 * h).UnixMilli(), "end_at": time.Now().Add(-6*h + 14*time.Minute).UnixMilli(),
		"title": "Crew meeting", "participants": []string{uKhoa, uLan, me}}
	plan := s.add("c-chithi", uLan, "@luffy can you sign off on the landing plan? the Marine blockade part is tight", 3*h, "")
	s.add("c-chithi", me, "leave it to me! shishishi", 2*h, plan["id"].(string))
	s.add("c-chithi", uLan, "thank you, captain", 2*h-time.Minute, plan["id"].(string))
	plan["reply_count"] = 2
	s.add("c-chithi", uDuc, "Engine log:\n```\ncola tank: 12%\ncoup de burst: offline\nrerouting power ...\n```", 40*time.Minute, "")
	s.add("c-chithi", uDuc, "SUPER fix coming up", 39*time.Minute, "")
	radar := s.add("c-chithi", uKhoa, "Weather chart for the next island, details at https://example.com/chart", 30*time.Minute, "")
	radar["file_ids"] = []string{"f-radar"}
	radar["metadata"] = obj{
		"files": []obj{{"id": "f-radar", "name": "weather-chart.png", "mime_type": "image/png", "width": 360, "height": 220}},
		"reactions": []obj{
			{"user_id": me, "post_id": radar["id"], "emoji_name": "+1", "create_at": 1},
			{"user_id": uLan, "post_id": radar["id"], "emoji_name": "+1", "create_at": 2},
			{"user_id": uDuc, "post_id": radar["id"], "emoji_name": "eyes", "create_at": 3},
		},
	}
	s.add("c-trinhsat", uDuc, "Marine warship off the port bow at 18:05", 50*time.Minute, "")
	s.add("c-trinhsat", uKhoa, "@luffy do we change course?", 10*time.Minute, "")
	live := s.add("c-lab", uLan, "robin started a call", 5*time.Minute, "")
	live["type"] = "custom_calls"
	live["props"] = obj{"start_at": time.Now().Add(-5 * time.Minute).UnixMilli(), "title": "Checkup with Chopper"}
	s.add("c-kho", uDuc, "Treasure counted and locked away. Nami has the key.", 2*h, "")
	bounty := s.add("c-kho", uNews, "", 90*time.Minute, "")
	bounty["props"] = obj{"attachments": []obj{{
		"color":      "#d97706",
		"title":      "WANTED: Monkey D. Luffy",
		"title_link": "https://example.com/bounty/luffy",
		"text":       "Bounty raised after Egghead. [Open the poster](https://example.com/bounty/luffy)",
		"fields": []obj{
			{"title": "Crew", "value": "Straw Hat Pirates", "short": true},
			{"title": "Bounty", "value": "3,000,000,000 berries", "short": true},
			{"title": "Status", "value": "at large", "short": true},
			{"title": "Last seen", "value": "Elbaf", "short": true},
		},
		"footer": "World Government · Notice #80",
		"actions": []obj{
			{"id": "assign", "type": "select", "name": "Assign to..."},
			{"id": "ack", "type": "button", "name": "✅ Acknowledge", "style": "good"},
			{"id": "snooze", "type": "button", "name": "💤 Snooze 1h"},
			{"id": "burn", "type": "button", "name": "🗑 Burn it", "style": "danger"},
		},
	}}}
	for i, m := range []string{"lunch is ready. ladies first ♥", "where's the sake", "I once ate a whole Sea King, true story", "which way is the galley", "same way as yesterday, moss head", "LUNCH!!"} {
		s.add("c-tangau", []string{uSanji, uZoro, uUsopp, uZoro, uSanji, uUsopp}[i], m, time.Duration(90-i*10)*time.Minute, "")
	}
	s.add("c-dem-town", uKhoa, "Welcome aboard the Thousand Sunny.", 48*h, "")
	s.add("c-dem-ops", uDuc, "Coup de Burst is fueled and ready", 3*h, "")
	s.add("c-dm-lan", uLan, "The poneglyph is translated. Library at 9?", 5*time.Minute, "")
	return s
}

// radarPNG draws a green radar screen for the image demo.
func radarPNG() []byte {
	const w, h = 360, 220
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	cx, cy := float64(w)/2, float64(h)/2
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx, dy := float64(x)-cx, float64(y)-cy
			d := math.Hypot(dx, dy)
			c := color.RGBA{6, 18, 10, 255}
			// sweep: brighter just behind the beam
			if a := math.Mod(math.Atan2(dy, dx)+2*math.Pi, 2*math.Pi); d < 100 && a > 5.2 && a < 6.2 {
				g := uint8(20 + (a-5.2)*90)
				c = color.RGBA{10, g, 25, 255}
			}
			for _, r := range []float64{25, 50, 75, 100} {
				if math.Abs(d-r) < 0.8 {
					c = color.RGBA{40, 200, 90, 255}
				}
			}
			if d < 100 && (math.Abs(dx) < 0.6 || math.Abs(dy) < 0.6) {
				c = color.RGBA{30, 140, 70, 255}
			}
			img.Set(x, y, c)
		}
	}
	for _, p := range [][2]float64{{52, -30}, {-40, 22}, {18, 61}, {-70, -45}} {
		for y := -3.0; y <= 3; y++ {
			for x := -3.0; x <= 3; x++ {
				if x*x+y*y <= 9 {
					img.Set(int(cx+p[0]+x), int(cy+p[1]+y), color.RGBA{255, 80, 60, 255})
				}
			}
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

var (
	videoReplies = []string{
		"aye aye, captain",
		"on it 🫡",
		"ok, reporting back in 5",
		"passed it to the crew",
		"watching the horizon, will ping you",
		"done. not a trace left for the Marines",
	}
	videoThreadReplies = []string{
		"course updated the way you wanted",
		"got it, reworking the blockade part",
		"ok, plan B it is",
	}
)

func (s *server) userName(id string) string { return s.users[id]["username"].(string) }

func (s *server) partner(channel string) string {
	for _, c := range s.channels {
		if c["id"] == channel && c["type"] == "D" {
			name := c["name"].(string)
			return strings.TrimPrefix(strings.TrimPrefix(name, me+"__"), "__"+me)
		}
	}
	return ""
}

func (s *server) typing(channel, root, user string) {
	s.broadcast(obj{"event": "typing", "data": obj{"user_id": user, "parent_id": root}, "broadcast": obj{"channel_id": channel}})
}

// say posts as a teammate after showing the typing indicator.
func (s *server) say(channel, root, user, msg string, mentions []string) obj {
	s.typing(channel, root, user)
	time.Sleep(time.Duration(1500+len(msg)*25) * time.Millisecond)
	s.mu.Lock()
	p := s.add(channel, user, msg, 0, root)
	ev := s.postedEvent(p, mentions)
	s.mu.Unlock()
	s.broadcast(ev)
	return p
}

func (s *server) react(post obj, user, emoji string) {
	s.mu.Lock()
	r := obj{"user_id": user, "post_id": post["id"], "emoji_name": emoji, "create_at": time.Now().UnixMilli()}
	md, _ := post["metadata"].(obj)
	if md == nil {
		md = obj{}
		post["metadata"] = md
	}
	rs, _ := md["reactions"].([]obj)
	md["reactions"] = append(rs, r)
	channel := post["channel_id"]
	s.mu.Unlock()
	raw, _ := json.Marshal(r)
	s.broadcast(obj{"event": "reaction_added", "data": obj{"reaction": string(raw)}, "broadcast": obj{"channel_id": channel}})
}

// respond answers a post of yours: someone types and replies, then a
// colleague reacts to it.
func (s *server) respond(p obj) {
	channel, root := p["channel_id"].(string), p["root_id"].(string)
	s.mu.Lock()
	s.turn++
	turn := s.turn
	s.mu.Unlock()
	team := []string{uKhoa, uLan, uDuc}
	who := team[turn%3]
	if dm := s.partner(channel); dm != "" {
		who = dm
	}
	time.Sleep(700 * time.Millisecond)
	msg := videoReplies[turn%len(videoReplies)]
	switch {
	case root != "":
		msg = videoThreadReplies[turn%len(videoThreadReplies)]
	case p["file_ids"] != nil:
		msg = "nice one, pinned it to the map"
	}
	s.say(channel, root, who, msg, nil)
	time.Sleep(1200 * time.Millisecond)
	s.react(p, team[(turn+1)%3], []string{"+1", "white_check_mark", "fire"}[turn%3])
}

// welcome has a teammate show up in a channel you just created.
func (s *server) welcome(channel string) {
	time.Sleep(2500 * time.Millisecond)
	s.say(channel, "", uKhoa, "All hands on deck. Awaiting orders, captain.", nil)
}

func (s *server) currentChannel() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == "" {
		return "c-chithi"
	}
	return s.current
}

func (s *server) latestOwnPost() obj {
	s.mu.Lock()
	defer s.mu.Unlock()
	var best obj
	for _, list := range s.posts {
		for _, p := range list {
			if p["user_id"] == me && (best == nil || p["create_at"].(int64) > best["create_at"].(int64)) {
				best = p
			}
		}
	}
	return best
}

const controlsHelp = `
  mmt video controls — keep this window off camera
  1  nami mentions you in #lookout      (notification)
  2  franky calls you                     (incoming call)
  3  robin sends a DM                     (try it while mmt is locked)
  4  a teammate types and posts in the channel you have open
  5  a teammate reacts to your latest message
  6  nami posts a new sea chart in the channel you have open
  q  quit
`

// controls reads single keys from the server's terminal.
func (s *server) controls() {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return
	}
	old, err := term.MakeRaw(fd)
	if err != nil {
		return
	}
	restore := func() { _ = term.Restore(fd, old) }
	say := func(f string, a ...any) { fmt.Print(strings.ReplaceAll(fmt.Sprintf(f, a...), "\n", "\r\n")) }
	say(controlsHelp + "\n> ")
	buf := make([]byte, 1)
	for {
		if _, err := os.Stdin.Read(buf); err != nil {
			restore()
			return
		}
		switch buf[0] {
		case '1':
			say("1: nami → #lookout\n> ")
			go s.say("c-trinhsat", "", uKhoa, "@luffy Marine warship off the starboard side, do we run?", []string{me})
		case '2':
			say("2: franky is calling\n> ")
			go s.incomingCall("c-dm-duc", uDuc)
		case '3':
			say("3: robin → DM\n> ")
			go s.say("c-dm-lan", "", uLan, "Found a Road Poneglyph. Coordinates are in the vault. Burn after reading.", nil)
		case '4':
			ch := s.currentChannel()
			say("4: post in %s\n> ", ch)
			who := uLan
			if dm := s.partner(ch); dm != "" {
				who = dm
			}
			go s.say(ch, "", who, "Status: all hands accounted for.", nil)
		case '5':
			if p := s.latestOwnPost(); p != nil {
				say("5: reaction\n> ")
				go s.react(p, uLan, "fire")
			} else {
				say("5: post something first\n> ")
			}
		case '6':
			ch := s.currentChannel()
			say("6: sea chart in %s\n> ", ch)
			go func() {
				s.typing(ch, "", uKhoa)
				time.Sleep(1500 * time.Millisecond)
				s.mu.Lock()
				p := s.add(ch, uKhoa, "New sea chart", 0, "")
				p["file_ids"] = []string{"f-radar"}
				p["metadata"] = obj{"files": []obj{{"id": "f-radar", "name": "sea-chart.png", "mime_type": "image/png", "width": 360, "height": 220}}}
				ev := s.postedEvent(p, nil)
				s.mu.Unlock()
				s.broadcast(ev)
			}()
		case 'q', 3: // q or Ctrl+C
			restore()
			fmt.Println()
			os.Exit(0)
		}
	}
}

// incomingCall starts a call in a DM: the call card plus the Calls plugin's
// call_start event, which makes mmt show "is calling you".
func (s *server) incomingCall(channel, user string) {
	s.mu.Lock()
	p := s.add(channel, user, s.userName(user)+" started a call", 0, "")
	p["type"] = "custom_calls"
	p["props"] = obj{"start_at": time.Now().UnixMilli(), "call_status": "calling"}
	ev := s.postedEvent(p, nil)
	s.mu.Unlock()
	s.broadcast(ev)
	s.broadcast(obj{"event": "custom_com.mattermost.calls_call_start", "broadcast": obj{"channel_id": channel},
		"data": obj{"id": "call-" + p["id"].(string), "channelID": channel, "owner_id": user, "start_at": time.Now().UnixMilli()}})
}
