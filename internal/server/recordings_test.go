package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// pvrBox serves anonymised TV and PVR answers and records the writes.
type pvrBox struct {
	writes []string // "METHOD path body"
}

func (b *pvrBox) Get(_ context.Context, path string, out any) error {
	now := time.Now().Unix()
	var body string
	switch {
	case path == "tv/bouquets/":
		body = `[{"id":1,"channel_count":3,"name":"Freebox TV","state":"ready"}]`
	case path == "tv/bouquets/1/channels/":
		body = `[{"number":2,"uuid":"uuid-webtv-2","available":true},{"number":1,"uuid":"uuid-webtv-1","available":true},{"number":9,"uuid":"uuid-webtv-9","available":false}]`
	case path == "tv/channels/":
		body = `{"uuid-webtv-1":{"name":"Chaine Un"},"uuid-webtv-2":{"name":"Chaine Deux"},"uuid-webtv-9":{"name":"Chaine Neuf"}}`
	case strings.HasPrefix(path, "tv/epg/by_time/"):
		body = fmt.Sprintf(`{"uuid-webtv-1":{
			"%[1]d_aaa":{"title":"Journal","sub_title":"Edition du soir","category_name":"Info","date":%[1]d,"duration":1800,"next":"%[2]d_bbb"},
			"%[2]d_bbb":{"title":"Film du soir","date":%[2]d,"duration":5400,"next":""}}}`, now-600, now+1200)
	case path == "pvr/config/":
		body = `{"margin_before":2,"margin_after":5}`
	case path == "pvr/quota/":
		body = `{"quota_exceeded":true,"needed_tresh":8,"cur_tresh":0}`
	case path == "pvr/media/":
		body = `[{"media":"Disque test","free_bytes":50000000000,"total_bytes":100000000000}]`
	case path == "pvr/programmed/":
		body = `[{"id":7,"name":"Match","subname":"Finale","channel_name":"Chaine Un","channel_uuid":"uuid-webtv-1","start":1800000000,"end":1800003600,"state":"waiting_start_time","error":"none","enabled":true,"has_record_gen":false,"conflict":false},
			{"id":8,"name":"Serie generee","channel_name":"Chaine Deux","start":1800010000,"end":1800013600,"state":"waiting_start_time","error":"none","enabled":true,"has_record_gen":true}]`
	case path == "pvr/finished/":
		body = `[{"id":"5","name":"Vieux film","channel_name":"Chaine Deux","start":1700000000,"end":1700005400,"byte_size":2147483648,"state":"finished","error":"none","altered":false}]`
	default:
		return errors.New("unexpected path " + path)
	}
	return json.Unmarshal([]byte(body), out)
}

func (b *pvrBox) Do(ctx context.Context, method, path string, in, out any) error {
	if method == http.MethodGet {
		return b.Get(ctx, path, out)
	}
	j, _ := json.Marshal(in)
	b.writes = append(b.writes, method+" "+path+" "+string(j))
	return nil
}

func (b *pvrBox) Permissions(context.Context) (map[string]bool, error) {
	return map[string]bool{"settings": true}, nil
}

func pvrClient(t *testing.T) (*pvrBox, *http.Client, string, func(path string, f url.Values)) {
	t.Helper()
	box := &pvrBox{}
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	return box, c, srv.URL, func(path string, f url.Values) {
		t.Helper()
		resp := post(t, c, srv, path, f)
		if resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("POST %s: %d", path, resp.StatusCode)
		}
	}
}

func TestTVPage(t *testing.T) {
	box := &pvrBox{}
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	resp, body := get(t, c, srv, "/tv")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d\n%s", resp.StatusCode, body)
	}
	for _, want := range []string{"Chaine Un", "Chaine Deux", "Journal", "Edition du soir", "Film du soir", `name="channel" value="uuid-webtv-1"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(body, "Chaine Neuf") {
		t.Error("unavailable channel shown by default")
	}
	if strings.Index(body, "Chaine Un") > strings.Index(body, "Chaine Deux") {
		t.Error("channels not sorted by number")
	}
	if _, body := get(t, c, srv, "/tv?all=1&q=neuf"); !strings.Contains(body, "Chaine Neuf") || strings.Contains(body, "Chaine Un") {
		t.Error("filter all+q wrong")
	}
}

func TestTVRecord(t *testing.T) {
	box, _, _, do := pvrClient(t)
	now := time.Now().Unix()
	do("/tv/record", url.Values{"channel": {"uuid-webtv-1"}, "start": {fmt.Sprint(now + 600)}, "end": {fmt.Sprint(now + 4200)}, "name": {"Film du soir"}, "subname": {""}})
	want := fmt.Sprintf(`POST pvr/programmed/ {"channel_uuid":"uuid-webtv-1","end":%d,"name":"Film du soir","start":%d,"subname":""}`, now+4200, now+600)
	if len(box.writes) != 1 || box.writes[0] != want {
		t.Fatalf("writes = %v\nwant %s", box.writes, want)
	}
	// A program already over, or without a channel, sends nothing.
	box.writes = nil
	do("/tv/record", url.Values{"channel": {"uuid-webtv-1"}, "start": {"100"}, "end": {"200"}})
	do("/tv/record", url.Values{"start": {fmt.Sprint(now)}, "end": {fmt.Sprint(now + 60)}})
	if len(box.writes) != 0 {
		t.Fatalf("unexpected writes %v", box.writes)
	}
}

func TestRecordingsPage(t *testing.T) {
	srv := newTestServer(t, &pvrBox{})
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	resp, body := get(t, c, srv, "/recordings")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d\n%s", resp.StatusCode, body)
	}
	for _, want := range []string{"Match", "Finale", "Serie generee", "Vieux film", "2.0 Go", "Disque test", "50 % utilisés", "palier 0 / 8",
		`value="2"`, `value="5"`, "/recordings/programmed/7/update", "/recordings/finished/5/delete", `name="confirm" value="1" required`, "1 · Chaine Un"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(body, "/recordings/programmed/8/update") {
		t.Error("generated record must not be editable")
	}
}

func TestRecordingsWrites(t *testing.T) {
	box, _, _, do := pvrClient(t)
	local := func(h int) string { return time.Date(2030, 1, 2, h, 30, 0, 0, time.Local).Format(inputTime) }
	at := func(h int) int64 { return time.Date(2030, 1, 2, h, 30, 0, 0, time.Local).Unix() }

	do("/recordings/config", url.Values{"margin_before": {"3"}, "margin_after": {"10"}})
	do("/recordings/quota", nil)
	do("/recordings/programmed", url.Values{"channel": {"uuid-webtv-2"}, "name": {"Test"}, "subname": {"S"}, "start": {local(20)}, "end": {local(21)}})
	do("/recordings/programmed/7/update", url.Values{"name": {"Nouveau"}, "subname": {""}, "start": {local(20)}, "end": {local(22)}})
	do("/recordings/programmed/8/enabled", url.Values{"enabled": {"0"}})
	do("/recordings/programmed/7/delete", url.Values{"confirm": {"1"}})
	do("/recordings/finished/5/rename", url.Values{"name": {"Renommé"}, "subname": {"x"}})
	do("/recordings/finished/5/delete", url.Values{"confirm": {"1"}})
	want := []string{
		`PUT pvr/config/ {"margin_after":10,"margin_before":3}`,
		`PUT pvr/quota/ {}`,
		fmt.Sprintf(`POST pvr/programmed/ {"channel_uuid":"uuid-webtv-2","end":%d,"name":"Test","start":%d,"subname":"S"}`, at(21), at(20)),
		fmt.Sprintf(`PUT pvr/programmed/7/ {"end":%d,"name":"Nouveau","start":%d,"subname":""}`, at(22), at(20)),
		`PUT pvr/programmed/8/ {"enabled":false}`,
		`DELETE pvr/programmed/7/ null`,
		`PUT pvr/finished/5/ {"name":"Renommé","subname":"x"}`,
		`DELETE pvr/finished/5/ null`,
	}
	if strings.Join(box.writes, "\n") != strings.Join(want, "\n") {
		t.Fatalf("writes:\n%s\nwant:\n%s", strings.Join(box.writes, "\n"), strings.Join(want, "\n"))
	}
}

func TestRecordingsRefusals(t *testing.T) {
	box, _, _, do := pvrClient(t)
	do("/recordings/finished/5/delete", nil) // dangerous, not confirmed
	do("/recordings/programmed/7/delete", nil)
	do("/recordings/config", url.Values{"margin_before": {"-1"}, "margin_after": {"0"}})
	do("/recordings/programmed", url.Values{"channel": {"uuid-webtv-2"}, "name": {"x"}, "start": {"2030-01-02T21:00"}, "end": {"2030-01-02T20:00"}})
	do("/recordings/programmed", url.Values{"channel": {"uuid-webtv-2"}, "name": {"x"}, "start": {"nope"}, "end": {"nope"}})
	do("/recordings/programmed/7/update", url.Values{"name": {"x"}})
	if len(box.writes) != 0 {
		t.Fatalf("unexpected writes %v", box.writes)
	}
}

// manyChannelsBox has more channels than the default page shows.
type manyChannelsBox struct{ pvrBox }

func (b *manyChannelsBox) Get(ctx context.Context, path string, out any) error {
	var chans []tvBouquetChannel
	names := map[string]tvChannelInfo{}
	for i := 1; i <= tvLimit+20; i++ {
		u := fmt.Sprintf("uuid-%d", i)
		chans = append(chans, tvBouquetChannel{Number: i, UUID: u, Available: true})
		names[u] = tvChannelInfo{Name: fmt.Sprintf("Chaine n%d", i)}
	}
	switch path {
	case "tv/bouquets/1/channels/":
		j, _ := json.Marshal(chans)
		return json.Unmarshal(j, out)
	case "tv/channels/":
		j, _ := json.Marshal(names)
		return json.Unmarshal(j, out)
	}
	return b.pvrBox.Get(ctx, path, out)
}

func TestTVLimitsChannels(t *testing.T) {
	srv := newTestServer(t, &manyChannelsBox{})
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	_, body := get(t, c, srv, "/tv")
	if !strings.Contains(body, "Chaine n100<") || strings.Contains(body, "Chaine n101<") || !strings.Contains(body, "Tout afficher (120)") {
		t.Error("default page must show 100 channels and a link to all")
	}
	if _, body := get(t, c, srv, "/tv?tout=1"); !strings.Contains(body, "Chaine n120<") || strings.Contains(body, "Tout afficher") {
		t.Error("tout=1 must show every channel")
	}
}
