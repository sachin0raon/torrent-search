package stream

import (
	"context"
	"strings"
	"sync"

	qbt "github.com/autobrr/go-qbittorrent"
)

// fakeQbtAPI is a hand-written test double for the qbtAPI interface — no live
// qBittorrent instance is needed to exercise qbt_client.go / qbt_reader.go.
type fakeQbtAPI struct {
	mu sync.Mutex

	loginErr error

	addErr    error
	added     []string
	addedOpts []map[string]string

	tagsCalls []tagsCall
	tagsErr   error

	torrents       map[string]qbt.Torrent // by hash
	getTorrentsErr error

	props    map[string]qbt.TorrentProperties
	propsErr error

	files    map[string]qbt.TorrentFiles
	filesErr error

	pieceStates      map[string][]qbt.PieceState
	pieceStatesErr   error
	pieceStatesCalls int

	priorityCalls []priorityCall
	priorityErr   error

	trackersCalls []trackersCall

	deleteCalls [][]string
	deleteErr   error

	categoryCalls []categoryCall
	categoryErr   error
}

type categoryCall struct {
	hashes   []string
	category string
}

type priorityCall struct {
	hash     string
	ids      string
	priority int
}

type trackersCall struct{ hash, urls string }

type tagsCall struct {
	hashes []string
	tags   string
}

func newFakeQbtAPI() *fakeQbtAPI {
	return &fakeQbtAPI{
		torrents:    make(map[string]qbt.Torrent),
		props:       make(map[string]qbt.TorrentProperties),
		files:       make(map[string]qbt.TorrentFiles),
		pieceStates: make(map[string][]qbt.PieceState),
	}
}

func (f *fakeQbtAPI) LoginCtx(ctx context.Context) error { return f.loginErr }

func (f *fakeQbtAPI) AddTorrentFromUrlCtx(ctx context.Context, url string, options map[string]string) (*qbt.TorrentAddResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.addErr != nil {
		return nil, f.addErr
	}
	f.added = append(f.added, url)
	f.addedOpts = append(f.addedOpts, options)
	return &qbt.TorrentAddResponse{}, nil
}

func (f *fakeQbtAPI) GetTorrentsCtx(ctx context.Context, o qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getTorrentsErr != nil {
		return nil, f.getTorrentsErr
	}
	var out []qbt.Torrent
	for _, t := range f.torrents {
		if o.Category != "" && t.Category != o.Category {
			continue
		}
		if len(o.Hashes) > 0 && !containsStr(o.Hashes, t.Hash) {
			continue
		}
		if o.Tag != "" && !containsExactTag(t.Tags, o.Tag) {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

func (f *fakeQbtAPI) GetTorrentPropertiesCtx(ctx context.Context, hash string) (qbt.TorrentProperties, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.propsErr != nil {
		return qbt.TorrentProperties{}, f.propsErr
	}
	return f.props[hash], nil
}

func (f *fakeQbtAPI) GetFilesInformationCtx(ctx context.Context, hash string) (*qbt.TorrentFiles, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.filesErr != nil {
		return nil, f.filesErr
	}
	files, ok := f.files[hash]
	if !ok {
		return nil, nil
	}
	return &files, nil
}

func (f *fakeQbtAPI) GetTorrentPieceStatesCtx(ctx context.Context, hash string) ([]qbt.PieceState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pieceStatesCalls++
	if f.pieceStatesErr != nil {
		return nil, f.pieceStatesErr
	}
	return f.pieceStates[hash], nil
}

func (f *fakeQbtAPI) getPieceStatesCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pieceStatesCalls
}

func (f *fakeQbtAPI) SetFilePriorityCtx(ctx context.Context, hash string, ids string, priority int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.priorityErr != nil {
		return f.priorityErr
	}
	f.priorityCalls = append(f.priorityCalls, priorityCall{hash, ids, priority})
	return nil
}

func (f *fakeQbtAPI) AddTrackersCtx(ctx context.Context, hash string, urls string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.trackersCalls = append(f.trackersCalls, trackersCall{hash, urls})
	return nil
}

// GetTorrentPeersCtx is only here so fakeQbtAPI also satisfies qbtPeerAPI
// (qbt_peers.go) — no test currently exercises real peer data, so this
// always returns an empty response.
func (f *fakeQbtAPI) GetTorrentPeersCtx(ctx context.Context, hash string, rid int64) (*qbt.TorrentPeersResponse, error) {
	return &qbt.TorrentPeersResponse{}, nil
}

func (f *fakeQbtAPI) DeleteTorrentsCtx(ctx context.Context, hashes []string, deleteFiles bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleteCalls = append(f.deleteCalls, hashes)
	return nil
}

func (f *fakeQbtAPI) PauseCtx(_ context.Context, hashes []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, h := range hashes {
		if t, ok := f.torrents[h]; ok {
			t.State = qbt.TorrentStatePausedDl
			f.torrents[h] = t
		}
	}
	return nil
}

func (f *fakeQbtAPI) ResumeCtx(_ context.Context, hashes []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, h := range hashes {
		if t, ok := f.torrents[h]; ok {
			t.State = qbt.TorrentStateDownloading
			f.torrents[h] = t
		}
	}
	return nil
}

// SetCategoryCtx records each call and, mirroring real qBittorrent, moves the
// torrent into the new category — used to assert Move to Downloads actually
// recategorizes rather than only calling the API.
func (f *fakeQbtAPI) SetCategoryCtx(ctx context.Context, hashes []string, category string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.categoryErr != nil {
		return f.categoryErr
	}
	f.categoryCalls = append(f.categoryCalls, categoryCall{hashes, category})
	for _, h := range hashes {
		if t, ok := f.torrents[h]; ok {
			t.Category = category
			f.torrents[h] = t
		}
	}
	return nil
}

func (f *fakeQbtAPI) getCategoryCalls() []categoryCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]categoryCall, len(f.categoryCalls))
	copy(out, f.categoryCalls)
	return out
}

// AddTagsCtx mirrors real qBittorrent's additive semantics: it appends to
// whatever tags a torrent already has rather than replacing them, so a
// season-pack torrent picked from by two different browser sessions ends up
// visible to both.
func (f *fakeQbtAPI) AddTagsCtx(ctx context.Context, hashes []string, tags string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.tagsErr != nil {
		return f.tagsErr
	}
	f.tagsCalls = append(f.tagsCalls, tagsCall{hashes, tags})
	for _, h := range hashes {
		t, ok := f.torrents[h]
		if !ok {
			continue
		}
		if t.Tags == "" {
			t.Tags = tags
		} else {
			t.Tags += "," + tags
		}
		f.torrents[h] = t
	}
	return nil
}

// containsExactTag mirrors go-qbittorrent's own filter.go helper of the same
// name: whether the comma-separated tags string includes target as a full
// token, not just a substring.
func containsExactTag(tags, target string) bool {
	if tags == "" || target == "" {
		return false
	}
	for _, tag := range strings.Split(tags, ",") {
		if strings.TrimSpace(tag) == target {
			return true
		}
	}
	return false
}

// getDeleteCallCount is the lock-guarded way to read deleteCalls' length from
// a goroutine other than the one driving the fake (e.g. a test polling for a
// background sweep to have run) — reading f.deleteCalls directly in that case
// races with DeleteTorrentsCtx's own locked write.
func (f *fakeQbtAPI) getDeleteCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.deleteCalls)
}

func (f *fakeQbtAPI) setPieceStates(hash string, states []qbt.PieceState) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pieceStates[hash] = states
}

func (f *fakeQbtAPI) getPriorityCalls() []priorityCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]priorityCall, len(f.priorityCalls))
	copy(out, f.priorityCalls)
	return out
}

func (f *fakeQbtAPI) getTagsCalls() []tagsCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]tagsCall, len(f.tagsCalls))
	copy(out, f.tagsCalls)
	return out
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
