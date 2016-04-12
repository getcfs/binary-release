package api

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gholt/flog"
	"github.com/gholt/ring"
	"github.com/gholt/store"
	"github.com/pandemicsyn/oort/oort"
	synpb "github.com/pandemicsyn/syndicate/api/proto"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

type ReplValueStore struct {
	logError                   func(string, ...interface{})
	logDebug                   func(string, ...interface{})
	logDebugOn                 bool
	addressIndex               int
	valueCap                   int
	concurrentRequestsPerStore int
	failedConnectRetryDelay    int
	grpcOpts                   []grpc.DialOption

	ringLock           sync.RWMutex
	ring               ring.Ring
	ringCachePath      string
	ringServer         string
	ringServerGRPCOpts []grpc.DialOption
	ringServerExitChan chan struct{}
	ringClientID       string

	storesLock sync.RWMutex
	stores     map[string]*replValueStoreAndTicketChan
}

type replValueStoreAndTicketChan struct {
	store      store.ValueStore
	ticketChan chan struct{}
}

func NewReplValueStore(c *ReplValueStoreConfig) *ReplValueStore {
	cfg := resolveReplValueStoreConfig(c)
	rs := &ReplValueStore{
		logError:                   cfg.LogError,
		logDebug:                   cfg.LogDebug,
		logDebugOn:                 cfg.LogDebug != nil,
		addressIndex:               cfg.AddressIndex,
		valueCap:                   int(cfg.ValueCap),
		concurrentRequestsPerStore: cfg.ConcurrentRequestsPerStore,
		failedConnectRetryDelay:    cfg.FailedConnectRetryDelay,
		grpcOpts:                   cfg.GRPCOpts,
		stores:                     make(map[string]*replValueStoreAndTicketChan),
		ringServer:                 cfg.RingServer,
		ringServerGRPCOpts:         cfg.RingServerGRPCOpts,
		ringCachePath:              cfg.RingCachePath,
		ringClientID:               cfg.RingClientID,
	}
	if rs.logError == nil {
		rs.logError = flog.Default.ErrorPrintf
	}
	if rs.logDebug == nil {
		rs.logDebug = func(string, ...interface{}) {}
	}
	if rs.ringCachePath != "" {
		if fp, err := os.Open(rs.ringCachePath); err != nil {
			rs.logDebug("replValueStore: error loading cached ring %q: %s", rs.ringCachePath, err)
		} else if r, err := ring.LoadRing(fp); err != nil {
			fp.Close()
			rs.logDebug("replValueStore: error loading cached ring %q: %s", rs.ringCachePath, err)
		} else {
			fp.Close()
			rs.ring = r
		}
	}
	return rs
}

func (rs *ReplValueStore) Ring() ring.Ring {
	rs.ringLock.RLock()
	r := rs.ring
	rs.ringLock.RUnlock()
	return r
}

func (rs *ReplValueStore) SetRing(r ring.Ring) {
	if r == nil {
		return
	}
	rs.ringLock.Lock()
	if rs.ringCachePath != "" {
		dir, name := path.Split(rs.ringCachePath)
		_ = os.MkdirAll(dir, 0755)
		fp, err := ioutil.TempFile(dir, name)
		if err != nil {
			rs.logDebug("replValueStore: error caching ring %q: %s", rs.ringCachePath, err)
		} else if err := r.Persist(fp); err != nil {
			fp.Close()
			os.Remove(fp.Name())
			rs.logDebug("replValueStore: error caching ring %q: %s", rs.ringCachePath, err)
		} else {
			fp.Close()
			if err := os.Rename(fp.Name(), rs.ringCachePath); err != nil {
				os.Remove(fp.Name())
				rs.logDebug("replValueStore: error caching ring %q: %s", rs.ringCachePath, err)
			}
		}
	}
	rs.ring = r
	var currentAddrs map[string]struct{}
	if r != nil {
		nodes := r.Nodes()
		currentAddrs = make(map[string]struct{}, len(nodes))
		for _, n := range nodes {
			currentAddrs[n.Address(rs.addressIndex)] = struct{}{}
		}
	}
	var shutdownAddrs []string
	rs.storesLock.RLock()
	for a := range rs.stores {
		if _, ok := currentAddrs[a]; !ok {
			shutdownAddrs = append(shutdownAddrs, a)
		}
	}
	rs.storesLock.RUnlock()
	if len(shutdownAddrs) > 0 {
		shutdownStores := make([]*replValueStoreAndTicketChan, len(shutdownAddrs))
		rs.storesLock.Lock()
		for i, a := range shutdownAddrs {
			shutdownStores[i] = rs.stores[a]
			rs.stores[a] = nil
		}
		rs.storesLock.Unlock()
		for i, s := range shutdownStores {
			if err := s.store.Shutdown(context.Background()); err != nil {
				rs.logDebug("replValueStore: error during shutdown of store %s: %s", shutdownAddrs[i], err)
			}
		}
	}
	rs.ringLock.Unlock()
}

func (rs *ReplValueStore) storesFor(ctx context.Context, keyA uint64) ([]*replValueStoreAndTicketChan, error) {
	rs.ringLock.RLock()
	r := rs.ring
	rs.ringLock.RUnlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	if r == nil {
		return nil, noRingErr
	}
	ns := r.ResponsibleNodes(uint32(keyA >> (64 - r.PartitionBitCount())))
	as := make([]string, len(ns))
	for i, n := range ns {
		as[i] = n.Address(rs.addressIndex)
	}
	ss := make([]*replValueStoreAndTicketChan, len(ns))
	var someNil bool
	rs.storesLock.RLock()
	for i := len(ss) - 1; i >= 0; i-- {
		ss[i] = rs.stores[as[i]]
		if ss[i] == nil {
			someNil = true
		}
	}
	rs.storesLock.RUnlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	if someNil {
		rs.storesLock.Lock()
		select {
		case <-ctx.Done():
			rs.storesLock.Unlock()
			return nil, ctx.Err()
		default:
		}
		for i := len(ss) - 1; i >= 0; i-- {
			if ss[i] == nil {
				ss[i] = rs.stores[as[i]]
				if ss[i] == nil {
					var err error
					tc := make(chan struct{}, rs.concurrentRequestsPerStore)
					for i := cap(tc); i > 0; i-- {
						tc <- struct{}{}
					}
					ss[i] = &replValueStoreAndTicketChan{ticketChan: tc}
					ss[i].store, err = NewValueStore(as[i], rs.concurrentRequestsPerStore, rs.grpcOpts...)
					if err != nil {
						ss[i].store = errorValueStore(fmt.Sprintf("could not create store for %s: %s", as[i], err))
						// Launch goroutine to clear out the error store after
						// some time so a retry will occur.
						go func(addr string) {
							time.Sleep(time.Duration(rs.failedConnectRetryDelay) * time.Second)
							rs.storesLock.Lock()
							s := rs.stores[addr]
							if s != nil {
								if _, ok := s.store.(errorValueStore); ok {
									rs.stores[addr] = nil
								}
							}
							rs.storesLock.Unlock()
						}(as[i])
					}
					rs.stores[as[i]] = ss[i]
					select {
					case <-ctx.Done():
						rs.storesLock.Unlock()
						return nil, ctx.Err()
					default:
					}
				}
			}
		}
		rs.storesLock.Unlock()
	}
	return ss, nil
}

func (rs *ReplValueStore) ringServerConnector(exitChan chan struct{}) {
	sleeperTicks := 2
	sleeperTicker := time.NewTicker(time.Second)
	sleeper := func() {
		for i := sleeperTicks; i > 0; i-- {
			select {
			case <-exitChan:
				break
			case <-sleeperTicker.C:
			}
		}
		if sleeperTicks < 60 {
			sleeperTicks *= 2
		}
	}
	for {
		select {
		case <-exitChan:
			break
		default:
		}
		ringServer := rs.ringServer
		if ringServer == "" {
			var err error
			ringServer, err = oort.GenServiceID("value", "syndicate", "tcp")
			if err != nil {
				rs.logError("replValueStore: error resolving ring service: %s", err)
				sleeper()
				continue
			}
		}
		conn, err := grpc.Dial(ringServer, rs.ringServerGRPCOpts...)
		if err != nil {
			rs.logError("replValueStore: error connecting to ring service %s: %s", ringServer, err)
			sleeper()
			continue
		}
		stream, err := synpb.NewSyndicateClient(conn).GetRingStream(context.Background(), &synpb.SubscriberID{Id: rs.ringClientID})
		if err != nil {
			rs.logError("replValueStore: error creating stream with ring service %s: %s", ringServer, err)
			sleeper()
			continue
		}
		connDoneChan := make(chan struct{})
		somethingICanTakeAnAddressOf := int32(0)
		activity := &somethingICanTakeAnAddressOf
		// This goroutine will detect when the exitChan is closed so it can
		// close the conn so that the blocking stream.Recv will get an error
		// and everything will unwind properly.
		// However, if the conn errors out on its own and exitChan isn't
		// closed, we're going to loop back around and try a new conn, but we
		// need to clear out this goroutine, which is what the connDoneChan is
		// for.
		// One last thing is that if nothing happens for fifteen minutes, we
		// can assume the conn has gone stale and close it, causing a loop
		// around to try a new conn.
		// It would be so much easier if Recv could use a timeout Context...
		go func(c *grpc.ClientConn, a *int32, cdc chan struct{}) {
			for {
				select {
				case <-exitChan:
				case <-cdc:
				case <-time.After(15 * time.Minute):
					// I'm comfortable with time.After here since it's just
					// once per fifteen minutes or new conn.
					v := atomic.LoadInt32(a)
					if v != 0 {
						atomic.AddInt32(a, -v)
						continue
					}
				}
				break
			}
			c.Close()
		}(conn, activity, connDoneChan)
		for {
			select {
			case <-exitChan:
				break
			default:
			}
			res, err := stream.Recv()
			if err != nil {
				rs.logDebug("replValueStore: error with stream to ring service %s: %s", ringServer, err)
				break
			}
			atomic.AddInt32(activity, 1)
			if res != nil {
				if r, err := ring.LoadRing(bytes.NewBuffer(res.Ring)); err != nil {
					rs.logDebug("replValueStore: error with ring received from stream to ring service %s: %s", ringServer, err)
				} else {
					// This will cache the ring if ringCachePath is not empty.
					rs.SetRing(r)
					// Resets the exponential sleeper since we had success.
					sleeperTicks = 2
					rs.logDebug("replValueStore: got new ring from stream to ring service %s: %d", ringServer, res.Version)
				}
			}
		}
		close(connDoneChan)
		sleeper()
	}
}

// Startup is not required to use the ReplValueStore; it will automatically
// connect to backend stores as needed. However, if you'd like to use the ring
// service to receive ring updates and have the ReplValueStore automatically
// update itself accordingly, Startup will launch a connector to that service.
// Otherwise, you will need to call SetRing yourself to inform the
// ReplValueStore of which backends to connect to.
func (rs *ReplValueStore) Startup(ctx context.Context) error {
	rs.ringLock.Lock()
	if rs.ringServerExitChan == nil {
		rs.ringServerExitChan = make(chan struct{})
		go rs.ringServerConnector(rs.ringServerExitChan)
	}
	rs.ringLock.Unlock()
	return nil
}

// Shutdown will close all connections to backend stores and shutdown any
// running ring service connector. Note that the ReplValueStore can still be
// used after Shutdown, it will just start reconnecting to backends again. To
// relaunch the ring service connector, you will need to call Startup.
func (rs *ReplValueStore) Shutdown(ctx context.Context) error {
	rs.ringLock.Lock()
	if rs.ringServerExitChan != nil {
		close(rs.ringServerExitChan)
		rs.ringServerExitChan = nil
	}
	rs.storesLock.Lock()
	for addr, stc := range rs.stores {
		if err := stc.store.Shutdown(ctx); err != nil {
			rs.logDebug("replValueStore: error during shutdown of store %s: %s", addr, err)
		}
		delete(rs.stores, addr)
		select {
		case <-ctx.Done():
			rs.storesLock.Unlock()
			return ctx.Err()
		default:
		}
	}
	rs.storesLock.Unlock()
	rs.ringLock.Unlock()
	return nil
}

func (rs *ReplValueStore) EnableWrites(ctx context.Context) error {
	return nil
}

func (rs *ReplValueStore) DisableWrites(ctx context.Context) error {
	return errors.New("cannot disable writes with this client at this time")
}

func (rs *ReplValueStore) Flush(ctx context.Context) error {
	return nil
}

func (rs *ReplValueStore) AuditPass(ctx context.Context) error {
	return errors.New("audit passes not available with this client at this time")
}

func (rs *ReplValueStore) Stats(ctx context.Context, debug bool) (fmt.Stringer, error) {
	return noStats, nil
}

func (rs *ReplValueStore) ValueCap(ctx context.Context) (uint32, error) {
	return uint32(rs.valueCap), nil
}

func (rs *ReplValueStore) Lookup(ctx context.Context, keyA, keyB uint64) (int64, uint32, error) {
	type rettype struct {
		timestampMicro int64
		length         uint32
		err            ReplValueStoreError
	}
	ec := make(chan *rettype)
	stores, err := rs.storesFor(ctx, keyA)
	if err != nil {
		return 0, 0, err
	}
	for _, s := range stores {
		go func(s *replValueStoreAndTicketChan) {
			ret := &rettype{}
			var err error
			select {
			case <-s.ticketChan:
				ret.timestampMicro, ret.length, err = s.store.Lookup(ctx, keyA, keyB)
				s.ticketChan <- struct{}{}
			case <-ctx.Done():
				err = ctx.Err()
			}
			if err != nil {
				ret.err = &replValueStoreError{store: s.store, err: err}
			}
			ec <- ret
		}(s)
	}
	var timestampMicro int64
	var length uint32
	var notFound bool
	var errs ReplValueStoreErrorSlice
	for _ = range stores {
		ret := <-ec
		if ret.timestampMicro > timestampMicro || timestampMicro == 0 {
			timestampMicro = ret.timestampMicro
			length = ret.length
			if ret.err != nil {
				notFound = store.IsNotFound(ret.err.Err())
			}
		}
		if ret.err != nil {
			errs = append(errs, ret.err)
		}
	}
	if notFound {
		nferrs := make(ReplValueStoreErrorNotFound, len(errs))
		for i, v := range errs {
			nferrs[i] = v
		}
		return timestampMicro, length, nferrs
	}
	if len(errs) < len(stores) {
		for _, err := range errs {
			rs.logDebug("replValueStore: error during lookup: %s", err)
		}
		errs = nil
	}
	if errs == nil {
		return timestampMicro, length, nil
	}
	return timestampMicro, length, errs
}

func (rs *ReplValueStore) Read(ctx context.Context, keyA uint64, keyB uint64, value []byte) (int64, []byte, error) {
	type rettype struct {
		timestampMicro int64
		value          []byte
		err            ReplValueStoreError
	}
	ec := make(chan *rettype)
	stores, err := rs.storesFor(ctx, keyA)
	if err != nil {
		return 0, nil, err
	}
	for _, s := range stores {
		go func(s *replValueStoreAndTicketChan) {
			ret := &rettype{}
			var err error
			select {
			case <-s.ticketChan:
				ret.timestampMicro, ret.value, err = s.store.Read(ctx, keyA, keyB, nil)
				s.ticketChan <- struct{}{}
			case <-ctx.Done():
				err = ctx.Err()
			}
			if err != nil {
				ret.err = &replValueStoreError{store: s.store, err: err}
			}
			ec <- ret
		}(s)
	}
	var timestampMicro int64
	var rvalue []byte
	var notFound bool
	var errs ReplValueStoreErrorSlice
	for _ = range stores {
		ret := <-ec
		if ret.timestampMicro > timestampMicro || timestampMicro == 0 {
			timestampMicro = ret.timestampMicro
			rvalue = ret.value
			if ret.err != nil {
				notFound = store.IsNotFound(ret.err.Err())
			}
		}
		if ret.err != nil {
			errs = append(errs, ret.err)
		}
	}
	if value != nil && rvalue != nil {
		rvalue = append(value, rvalue...)
	}
	if notFound {
		nferrs := make(ReplValueStoreErrorNotFound, len(errs))
		for i, v := range errs {
			nferrs[i] = v
		}
		return timestampMicro, rvalue, nferrs
	}
	if len(errs) < len(stores) {
		for _, err := range errs {
			rs.logDebug("replValueStore: error during read: %s", err)
		}
		errs = nil
	}
	if errs == nil {
		return timestampMicro, rvalue, nil
	}
	return timestampMicro, rvalue, errs
}

func (rs *ReplValueStore) Write(ctx context.Context, keyA uint64, keyB uint64, timestampMicro int64, value []byte) (int64, error) {
	if len(value) > rs.valueCap {
		return 0, fmt.Errorf("value length of %d > %d", len(value), rs.valueCap)
	}
	type rettype struct {
		oldTimestampMicro int64
		err               ReplValueStoreError
	}
	ec := make(chan *rettype)
	stores, err := rs.storesFor(ctx, keyA)
	if err != nil {
		return 0, err
	}
	for _, s := range stores {
		go func(s *replValueStoreAndTicketChan) {
			ret := &rettype{}
			var err error
			select {
			case <-s.ticketChan:
				ret.oldTimestampMicro, err = s.store.Write(ctx, keyA, keyB, timestampMicro, value)
				s.ticketChan <- struct{}{}
			case <-ctx.Done():
				err = ctx.Err()
			}
			if err != nil {
				ret.err = &replValueStoreError{store: s.store, err: err}
			}
			ec <- ret
		}(s)
	}
	var oldTimestampMicro int64
	var errs ReplValueStoreErrorSlice
	for _ = range stores {
		ret := <-ec
		if ret.err != nil {
			errs = append(errs, ret.err)
		} else if ret.oldTimestampMicro > oldTimestampMicro {
			oldTimestampMicro = ret.oldTimestampMicro
		}
	}
	if len(errs) < (len(stores)+1)/2 {
		for _, err := range errs {
			rs.logDebug("replValueStore: error during write: %s", err)
		}
		errs = nil
	}
	if errs == nil {
		return oldTimestampMicro, nil
	}
	return oldTimestampMicro, errs
}

func (rs *ReplValueStore) Delete(ctx context.Context, keyA uint64, keyB uint64, timestampMicro int64) (int64, error) {
	type rettype struct {
		oldTimestampMicro int64
		err               ReplValueStoreError
	}
	ec := make(chan *rettype)
	stores, err := rs.storesFor(ctx, keyA)
	if err != nil {
		return 0, err
	}
	for _, s := range stores {
		go func(s *replValueStoreAndTicketChan) {
			ret := &rettype{}
			var err error
			select {
			case <-s.ticketChan:
				ret.oldTimestampMicro, err = s.store.Delete(ctx, keyA, keyB, timestampMicro)
				s.ticketChan <- struct{}{}
			case <-ctx.Done():
				err = ctx.Err()
			}
			if err != nil {
				ret.err = &replValueStoreError{store: s.store, err: err}
			}
			ec <- ret
		}(s)
	}
	var oldTimestampMicro int64
	var errs ReplValueStoreErrorSlice
	for _ = range stores {
		ret := <-ec
		if ret.err != nil {
			errs = append(errs, ret.err)
		} else if ret.oldTimestampMicro > oldTimestampMicro {
			oldTimestampMicro = ret.oldTimestampMicro
		}
	}
	if len(errs) < (len(stores)+1)/2 {
		for _, err := range errs {
			rs.logDebug("replValueStore: error during delete: %s", err)
		}
		errs = nil
	}
	if errs == nil {
		return oldTimestampMicro, nil
	}
	return oldTimestampMicro, errs
}

type ReplValueStoreError interface {
	error
	Store() store.ValueStore
	Err() error
}

type ReplValueStoreErrorSlice []ReplValueStoreError

func (es ReplValueStoreErrorSlice) Error() string {
	if len(es) <= 0 {
		return "unknown error"
	} else if len(es) == 1 {
		return es[0].Error()
	}
	return fmt.Sprintf("%d errors, first is: %s", len(es), es[0])
}

type ReplValueStoreErrorNotFound ReplValueStoreErrorSlice

func (e ReplValueStoreErrorNotFound) Error() string {
	if len(e) <= 0 {
		return "not found"
	} else if len(e) == 1 {
		return e[0].Error()
	}
	return fmt.Sprintf("%d errors, first is: %s", len(e), e[0])
}

func (e ReplValueStoreErrorNotFound) ErrNotFound() string {
	return e.Error()
}

type replValueStoreError struct {
	store store.ValueStore
	err   error
}

func (e *replValueStoreError) Error() string {
	if e.err == nil {
		return "unknown error"
	}
	return e.err.Error()
}

func (e *replValueStoreError) Store() store.ValueStore {
	return e.store
}

func (e *replValueStoreError) Err() error {
	return e.err
}
