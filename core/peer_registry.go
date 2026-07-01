package core

import "sync/atomic"

// PeerRegistry publishes a read-only snapshot of daemon-local bot identities.
// Hot paths resolve names with a single atomic load and map lookup.
type PeerRegistry struct {
	snapshot atomic.Pointer[PeerSnapshot]
}

type PeerSnapshot struct {
	ByAppID map[string]PeerInfo
}

type PeerInfo struct {
	AppID       string
	Project     string
	Fallback    string
	APIName     string
	APIResolved bool
}

func NewPeerRegistry(seed map[string]string) *PeerRegistry {
	r := &PeerRegistry{}
	r.ResetFromConfig(seed)
	return r
}

func (r *PeerRegistry) Resolve(appID string) (string, bool) {
	if r == nil || appID == "" {
		return "", false
	}
	snapshot := r.snapshot.Load()
	if snapshot == nil {
		return "", false
	}
	info, ok := snapshot.ByAppID[appID]
	if !ok {
		return "", false
	}
	if info.APIName != "" {
		return info.APIName, true
	}
	if info.Fallback != "" {
		return info.Fallback, true
	}
	return "", false
}

func (r *PeerRegistry) ResetFromConfig(seed map[string]string) {
	next := make(map[string]PeerInfo, len(seed))
	current := r.snapshot.Load()
	for appID, project := range seed {
		if appID == "" {
			continue
		}
		info := PeerInfo{
			AppID:    appID,
			Project:  project,
			Fallback: project,
		}
		if current != nil {
			if prev, ok := current.ByAppID[appID]; ok {
				info.APIName = prev.APIName
				info.APIResolved = prev.APIResolved
			}
		}
		next[appID] = info
	}
	r.snapshot.Store(&PeerSnapshot{ByAppID: next})
}

func (r *PeerRegistry) UpdateAPIName(appID, name string) {
	if r == nil || appID == "" || name == "" {
		return
	}
	current := r.snapshot.Load()
	next := map[string]PeerInfo{}
	if current != nil {
		next = make(map[string]PeerInfo, len(current.ByAppID)+1)
		for k, v := range current.ByAppID {
			next[k] = v
		}
	}
	info := next[appID]
	info.AppID = appID
	info.APIName = name
	info.APIResolved = true
	if info.Project == "" {
		info.Project = info.Fallback
	}
	next[appID] = info
	r.snapshot.Store(&PeerSnapshot{ByAppID: next})
}

func (r *PeerRegistry) Stats() (total, apiResolved, fallback int) {
	if r == nil {
		return 0, 0, 0
	}
	snapshot := r.snapshot.Load()
	if snapshot == nil {
		return 0, 0, 0
	}
	total = len(snapshot.ByAppID)
	for _, info := range snapshot.ByAppID {
		if info.APIResolved && info.APIName != "" {
			apiResolved++
		} else if info.Fallback != "" {
			fallback++
		}
	}
	return total, apiResolved, fallback
}
