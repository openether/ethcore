package downloader

import (
	"context"
	"sync"

	"math/big"

	"github.com/openether/ethcore/common"
	"github.com/openether/ethcore/event"
	"github.com/openether/ethcore/rpc"
)

type DoneEvent struct {
	Peer *peer
	Hash common.Hash
	TD   *big.Int
}
type StartEvent struct {
	Peer *peer
	Hash common.Hash
	TD   *big.Int
}
type FailedEvent struct {
	Peer *peer
	Err  error
}

// PublicDownloaderAPI provides an API which gives information about the current synchronisation status.
// It offers only methods that operates on data that can be available to anyone without security risks.
type PublicDownloaderAPI struct {
	d                   *Downloader
	mux                 *event.TypeMux
	muSyncSubscriptions sync.Mutex
	syncSubscriptions   map[string]rpc.Subscription
}

// NewPublicDownloaderAPI create a new PublicDownloaderAPI.
func NewPublicDownloaderAPI(d *Downloader, m *event.TypeMux) *PublicDownloaderAPI {
	api := &PublicDownloaderAPI{d: d, mux: m, syncSubscriptions: make(map[string]rpc.Subscription)}

	go api.run()

	return api
}

func (api *PublicDownloaderAPI) run() {
	sub := api.mux.Subscribe(StartEvent{}, DoneEvent{}, FailedEvent{})

	for event := range sub.Chan() {
		var notification interface{}

		switch event.Data.(type) {
		case StartEvent:
			result := &SyncingResult{Syncing: true}
			result.Status.Origin, result.Status.Current, result.Status.Height, result.Status.Pulled, result.Status.Known = api.d.Progress()
			notification = result
		case DoneEvent, FailedEvent:
			notification = false
		}

		api.muSyncSubscriptions.Lock()
		for id, sub := range api.syncSubscriptions {
			if sub.Notify(notification) == rpc.ErrNotificationNotFound {
				delete(api.syncSubscriptions, id)
			}
		}
		api.muSyncSubscriptions.Unlock()
	}
}

// Progress gives progress indications when the node is synchronising with the Ethereum network.
type Progress struct {
	Origin  uint64 `json:"startingBlock"`
	Current uint64 `json:"currentBlock"`
	Height  uint64 `json:"highestBlock"`
	Pulled  uint64 `json:"pulledStates"`
	Known   uint64 `json:"knownStates"`
}

// SyncingResult provides information about the current synchronisation status for this node.
type SyncingResult struct {
	Syncing bool     `json:"syncing"`
	Status  Progress `json:"status"`
}

// Syncing provides information when this nodes starts synchronising with the Ethereum network and when it's finished.
func (api *PublicDownloaderAPI) Syncing(ctx context.Context) (rpc.Subscription, error) {
	notifier, supported := rpc.NotifierFromContext(ctx)
	if !supported {
		return nil, rpc.ErrNotificationsUnsupported
	}

	subscription, err := notifier.NewSubscription(func(id string) {
		api.muSyncSubscriptions.Lock()
		delete(api.syncSubscriptions, id)
		api.muSyncSubscriptions.Unlock()
	})

	if err != nil {
		return nil, err
	}

	api.muSyncSubscriptions.Lock()
	api.syncSubscriptions[subscription.ID()] = subscription
	api.muSyncSubscriptions.Unlock()

	return subscription, nil
}
