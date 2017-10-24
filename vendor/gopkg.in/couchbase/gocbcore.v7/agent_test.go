package gocbcore

import (
	"bytes"
	"flag"
	"fmt"
	"gopkg.in/couchbaselabs/gojcbmock.v1"
	"math"
	"os"
	"testing"
	"time"
)

// Gets a set of keys evenly distributed across all server nodes.
// the result is an array of strings, each index representing an index
// of a server
func (agent *Agent) makeDistKeys() (keys []string) {
	// Get the routing information
	cfg := agent.routingInfo.Get()
	keys = make([]string, cfg.clientMux.NumPipelines())
	remaining := len(keys)

	for i := 0; remaining > 0; i++ {
		keyTmp := fmt.Sprintf("DistKey_%d", i)
		// Map the vBucket and server
		vbID := cfg.vbMap.VbucketByKey([]byte(keyTmp))
		srvIx, err := cfg.vbMap.NodeByVbucket(vbID, 0)
		if err != nil || srvIx < 0 || srvIx >= len(keys) || keys[srvIx] != "" {
			continue
		}
		keys[srvIx] = keyTmp
		remaining--
	}
	return
}

func saslAuthFn(bucket, password string) func(AuthClient, time.Time) error {
	return func(srv AuthClient, deadline time.Time) error {
		// Build PLAIN auth data
		userBuf := []byte(bucket)
		passBuf := []byte(password)
		authData := make([]byte, 1+len(userBuf)+1+len(passBuf))
		authData[0] = 0
		copy(authData[1:], userBuf)
		authData[1+len(userBuf)] = 0
		copy(authData[1+len(userBuf)+1:], passBuf)

		// Execute PLAIN authentication
		_, err := srv.ExecSaslAuth([]byte("PLAIN"), authData, deadline)

		return err
	}
}

type Signaler struct {
	t      *testing.T
	signal chan int
}

func (s *Signaler) Continue() {
	s.signal <- 0
}

func (s *Signaler) Wrap(fn func()) {
	defer func() {
		if r := recover(); r != nil {
			// Rethrow actual panics
			if r != s {
				panic(r)
			}
		}
	}()
	fn()
	s.signal <- 0
}

func (s *Signaler) Fatalf(fmt string, args ...interface{}) {
	s.t.Logf(fmt, args...)
	s.signal <- 1
	panic(s)
}

func (s *Signaler) Skipf(fmt string, args ...interface{}) {
	s.t.Logf(fmt, args...)
	s.signal <- 2
	panic(s)
}

func (s *Signaler) Wait(waitSecs int) {
	if waitSecs <= 0 {
		waitSecs = 5
	}

	select {
	case v := <-s.signal:
		if v == 1 {
			s.t.FailNow()
		} else if v == 2 {
			s.t.SkipNow()
		}
	case <-time.After(time.Duration(waitSecs) * time.Second):
		s.t.Fatalf("Wait timeout expired")
	}
}

func (s *Signaler) TimeTravel(waitDura time.Duration) {
	waitSecs := int(math.Ceil(float64(waitDura) / float64(time.Second)))

	// TODO: Implement real server support here
	globalMock.Control(gojcbmock.NewCommand(gojcbmock.CTimeTravel, map[string]interface{}{
		"Offset": waitSecs,
	}))
}

func getSignaler(t *testing.T) *Signaler {
	signaler := &Signaler{
		t:      t,
		signal: make(chan int),
	}
	return signaler
}

var globalAgent *Agent
var globalMemdAgent *Agent
var globalMock *gojcbmock.Mock

func getAgent(t *testing.T) *Agent {
	return globalAgent
}

func TestHttpAgent(t *testing.T) {
	agentConfig := &AgentConfig{
		MemdAddrs:            []string{},
		HttpAddrs:            []string{fmt.Sprintf("127.0.0.1:%d", globalMock.EntryPort)},
		TlsConfig:            nil,
		BucketName:           "default",
		Password:             "",
		AuthHandler:          saslAuthFn("default", ""),
		ConnectTimeout:       5 * time.Second,
		ServerConnectTimeout: 1 * time.Second,
	}

	agent, err := CreateAgent(agentConfig)
	if err != nil {
		t.Fatalf("Failed to connect to server")
	}
	logDebugf("Agent created...")
	agent.Close()
}

func getAgentnSignaler(t *testing.T) (*Agent, *Signaler) {
	return getAgent(t), getSignaler(t)
}

func TestBasicOps(t *testing.T) {
	agent, s := getAgentnSignaler(t)

	// Set
	agent.Set([]byte("test"), []byte("{}"), 0, 0, func(cas Cas, mt MutationToken, err error) {
		s.Wrap(func() {
			if err != nil {
				s.Fatalf("Set operation failed")
			}
			if cas == Cas(0) {
				s.Fatalf("Invalid cas received")
			}
		})
	})
	s.Wait(0)

	// Get
	agent.Get([]byte("test"), func(value []byte, flags uint32, cas Cas, err error) {
		s.Wrap(func() {
			if err != nil {
				s.Fatalf("Get operation failed")
			}
			if cas == Cas(0) {
				s.Fatalf("Invalid cas received")
			}
		})
	})
	s.Wait(0)

	// GetReplica Specific
	agent.GetReplica([]byte("test"), 1, func(value []byte, flags uint32, cas Cas, err error) {
		s.Wrap(func() {
			if err != nil {
				s.Fatalf("Get operation failed")
			}
			if cas == Cas(0) {
				s.Fatalf("Invalid cas received")
			}
		})
	})
	s.Wait(0)

	// GetReplica Any
	agent.GetReplica([]byte("test"), 0, func(value []byte, flags uint32, cas Cas, err error) {
		s.Wrap(func() {
			if err != nil {
				s.Fatalf("Get operation failed")
			}
			if cas == Cas(0) {
				s.Fatalf("Invalid cas received")
			}
		})
	})
	s.Wait(0)
}

func TestBasicReplace(t *testing.T) {
	agent, s := getAgentnSignaler(t)

	oldCas := Cas(0)
	agent.Set([]byte("testx"), []byte("{}"), 0, 0, func(cas Cas, mt MutationToken, err error) {
		oldCas = cas
		s.Continue()
	})
	s.Wait(0)

	agent.Replace([]byte("testx"), []byte("[]"), 0, oldCas, 0, func(cas Cas, mt MutationToken, err error) {
		s.Wrap(func() {
			if err != nil {
				s.Fatalf("Replace operation failed")
			}
			if cas == Cas(0) {
				s.Fatalf("Invalid cas received")
			}
		})
	})
	s.Wait(0)
}

func TestBasicRemove(t *testing.T) {
	agent, s := getAgentnSignaler(t)

	agent.Set([]byte("testy"), []byte("{}"), 0, 0, func(cas Cas, mt MutationToken, err error) {
		s.Continue()
	})
	s.Wait(0)

	agent.Remove([]byte("testy"), 0, func(cas Cas, mt MutationToken, err error) {
		s.Wrap(func() {
			if err != nil {
				s.Fatalf("Remove operation failed")
			}
		})
	})
	s.Wait(0)
}

func TestBasicInsert(t *testing.T) {
	agent, s := getAgentnSignaler(t)

	agent.Remove([]byte("testz"), 0, func(cas Cas, mt MutationToken, err error) {
		s.Continue()
	})
	s.Wait(0)

	agent.Add([]byte("testz"), []byte("[]"), 0, 0, func(cas Cas, mt MutationToken, err error) {
		s.Wrap(func() {
			if err != nil {
				t.Fatalf("Add operation failed")
			}
			if cas == Cas(0) {
				t.Fatalf("Invalid cas received")
			}
		})
	})
	s.Wait(0)
}

func TestBasicCounters(t *testing.T) {
	agent, s := getAgentnSignaler(t)

	// Counters
	agent.Remove([]byte("testCounters"), 0, func(cas Cas, mt MutationToken, err error) {
		s.Continue()
	})
	s.Wait(0)

	agent.Increment([]byte("testCounters"), 5, 11, 0, func(val uint64, cas Cas, mt MutationToken, err error) {
		s.Wrap(func() {
			if err != nil {
				s.Fatalf("Increment operation failed")
			}
			if cas == Cas(0) {
				s.Fatalf("Invalid cas received")
			}
			if val != 11 {
				s.Fatalf("Increment did not operate properly")
			}
		})
	})
	s.Wait(0)

	agent.Increment([]byte("testCounters"), 5, 22, 0, func(val uint64, cas Cas, mt MutationToken, err error) {
		s.Wrap(func() {
			if err != nil {
				s.Fatalf("Increment operation failed")
			}
			if cas == Cas(0) {
				s.Fatalf("Invalid cas received")
			}
			if val != 16 {
				s.Fatalf("Increment did not operate properly")
			}
		})
	})
	s.Wait(0)

	agent.Decrement([]byte("testCounters"), 3, 65, 0, func(val uint64, cas Cas, mt MutationToken, err error) {
		s.Wrap(func() {
			if err != nil {
				s.Fatalf("Increment operation failed")
			}
			if cas == Cas(0) {
				s.Fatalf("Invalid cas received")
			}
			if val != 13 {
				s.Fatalf("Increment did not operate properly")
			}
		})
	})
	s.Wait(0)
}

func TestBasicAdjoins(t *testing.T) {
	agent, s := getAgentnSignaler(t)

	agent.Set([]byte("testAdjoins"), []byte("there"), 0, 0, func(cas Cas, mt MutationToken, err error) {
		s.Continue()
	})
	s.Wait(0)

	agent.Append([]byte("testAdjoins"), []byte(" Frank!"), func(cas Cas, mt MutationToken, err error) {
		s.Wrap(func() {
			if err != nil {
				s.Fatalf("Append operation failed")
			}
			if cas == Cas(0) {
				s.Fatalf("Invalid cas received")
			}
		})
	})
	s.Wait(0)

	agent.Prepend([]byte("testAdjoins"), []byte("Hello "), func(cas Cas, mt MutationToken, err error) {
		s.Wrap(func() {
			if err != nil {
				s.Fatalf("Prepend operation failed")
			}
			if cas == Cas(0) {
				s.Fatalf("Invalid cas received")
			}
		})
	})
	s.Wait(0)

	agent.Get([]byte("testAdjoins"), func(value []byte, flags uint32, cas Cas, err error) {
		s.Wrap(func() {
			if err != nil {
				s.Fatalf("Get operation failed")
			}
			if cas == Cas(0) {
				s.Fatalf("Invalid cas received")
			}

			if string(value) != "Hello there Frank!" {
				s.Fatalf("Adjoin operations did not behave")
			}
		})
	})
	s.Wait(0)
}

func isKeyNotFoundError(err error) bool {
	te, ok := err.(interface {
		KeyNotFound() bool
	})
	return ok && te.KeyNotFound()
}

func TestExpiry(t *testing.T) {
	agent := getAgent(t)
	s := getSignaler(t)

	agent.Set([]byte("testExpiry"), []byte("{}"), 0, 1, func(cas Cas, mt MutationToken, err error) {
		s.Wrap(func() {
			if err != nil {
				s.Fatalf("Set operation failed")
			}
		})
	})
	s.Wait(0)

	s.TimeTravel(1500 * time.Millisecond)

	agent.Get([]byte("testExpiry"), func(value []byte, flags uint32, cas Cas, err error) {
		s.Wrap(func() {
			if !isKeyNotFoundError(err) {
				s.Fatalf("Get should have returned key not found")
			}
		})
	})
	s.Wait(0)
}

func TestTouch(t *testing.T) {
	agent := getAgent(t)
	s := getSignaler(t)

	agent.Set([]byte("testTouch"), []byte("{}"), 0, 1, func(cas Cas, mt MutationToken, err error) {
		s.Wrap(func() {
			if err != nil {
				s.Fatalf("Set operation failed")
			}
		})
	})
	s.Wait(0)

	agent.Touch([]byte("testTouch"), 0, 3, func(cas Cas, mt MutationToken, err error) {
		s.Wrap(func() {
			if err != nil {
				s.Fatalf("Touch operation failed")
			}
		})
	})
	s.Wait(0)

	s.TimeTravel(1500 * time.Millisecond)

	agent.Get([]byte("testTouch"), func(value []byte, flags uint32, cas Cas, err error) {
		s.Wrap(func() {
			if err != nil {
				s.Fatalf("Get should have been successful")
			}
		})
	})
	s.Wait(0)

	s.TimeTravel(2500 * time.Millisecond)

	agent.Get([]byte("testTouch"), func(value []byte, flags uint32, cas Cas, err error) {
		s.Wrap(func() {
			if !isKeyNotFoundError(err) {
				s.Fatalf("Get should have returned key not found")
			}
		})
	})
	s.Wait(0)
}

func TestGetAndTouch(t *testing.T) {
	agent := getAgent(t)
	s := getSignaler(t)

	agent.Set([]byte("testTouch"), []byte("{}"), 0, 1, func(cas Cas, mt MutationToken, err error) {
		s.Wrap(func() {
			if err != nil {
				s.Fatalf("Set operation failed")
			}
		})
	})
	s.Wait(0)

	agent.GetAndTouch([]byte("testTouch"), 3, func(value []byte, flags uint32, cas Cas, err error) {
		s.Wrap(func() {
			if err != nil {
				s.Fatalf("Touch operation failed")
			}
		})
	})
	s.Wait(0)

	s.TimeTravel(1500 * time.Millisecond)

	agent.Get([]byte("testTouch"), func(value []byte, flags uint32, cas Cas, err error) {
		s.Wrap(func() {
			if err != nil {
				s.Fatalf("Get should have been successful")
			}
		})
	})
	s.Wait(0)

	s.TimeTravel(2500 * time.Millisecond)

	agent.Get([]byte("testTouch"), func(value []byte, flags uint32, cas Cas, err error) {
		s.Wrap(func() {
			if !isKeyNotFoundError(err) {
				s.Fatalf("Get should have returned key not found")
			}
		})
	})
	s.Wait(0)
}

func TestObserve(t *testing.T) {
	agent := getAgent(t)
	s := getSignaler(t)

	agent.Set([]byte("testObserve"), []byte("there"), 0, 0, func(cas Cas, mt MutationToken, err error) {
		s.Continue()
	})
	s.Wait(0)

	agent.Observe([]byte("testObserve"), 1, func(ks KeyState, cas Cas, err error) {
		s.Wrap(func() {
			if err != nil {
				s.Fatalf("Get operation failed")
			}
		})
	})
	s.Wait(0)
}

func TestObserveSeqNo(t *testing.T) {
	agent, s := getAgentnSignaler(t)

	origMt := MutationToken{}
	agent.Set([]byte("testObserve"), []byte("there"), 0, 0, func(cas Cas, mt MutationToken, err error) {
		s.Wrap(func() {
			if err != nil {
				s.Fatalf("Initial set operation failed")
			}

			if mt.VbUuid == 0 && mt.SeqNo == 0 {
				s.Skipf("ObserveSeqNo not supported by server")
			}

			origMt = mt
		})
	})
	s.Wait(0)

	origCurSeqNo := SeqNo(0)
	agent.ObserveSeqNo([]byte("testObserve"), origMt.VbUuid, 1, func(curSeqNo, persistSeqNo SeqNo, err error) {
		s.Wrap(func() {
			if err != nil {
				s.Fatalf("ObserveSeqNo operation failed")
			}

			origCurSeqNo = curSeqNo
		})
	})
	s.Wait(0)

	newMt := MutationToken{}
	agent.Set([]byte("testObserve"), []byte("there"), 0, 0, func(cas Cas, mt MutationToken, err error) {
		s.Wrap(func() {
			if err != nil {
				s.Fatalf("Second set operation failed")
			}

			newMt = mt
		})
	})
	s.Wait(0)

	agent.ObserveSeqNo([]byte("testObserve"), newMt.VbUuid, 1, func(curSeqNo, persistSeqNo SeqNo, err error) {
		s.Wrap(func() {
			if err != nil {
				s.Fatalf("ObserveSeqNo operation failed")
			}
			if curSeqNo < origCurSeqNo {
				s.Fatalf("SeqNo does not appear to be working")
			}
		})
	})
	s.Wait(0)
}

func TestRandomGet(t *testing.T) {
	agent, s := getAgentnSignaler(t)
	distkeys := agent.makeDistKeys()
	for _, k := range distkeys {
		agent.Set([]byte(k), []byte("Hello World!"), 0, 0, func(cas Cas, mt MutationToken, err error) {
			s.Wrap(func() {
				if err != nil {
					s.Fatalf("Couldn't store some items: %v", err)
				}
			})
		})
		s.Wait(0)
	}

	agent.GetRandom(func(key, value []byte, flags uint32, cas Cas, err error) {
		s.Wrap(func() {
			if err != nil {
				s.Fatalf("Get operation failed: %v", err)
			}
			if cas == Cas(0) {
				s.Fatalf("Invalid cas received")
			}
			if len(key) == 0 {
				s.Fatalf("Invalid key returned")
			}
			if len(value) == 0 {
				s.Fatalf("No value returned")
			}
		})
	})
	s.Wait(0)
}

func TestSubdocXattrs(t *testing.T) {
	agent, s := getAgentnSignaler(t)

	agent.Set([]byte("testXattr"), []byte("{\"x\":\"xattrs\"}"), 0, 0, func(cas Cas, token MutationToken, err error) {
		s.Wrap(func() {
			if err != nil {
				s.Fatalf("Set operation failed: %v", err)
			}
		})
	})
	s.Wait(0)

	mutateOps := []SubDocOp{
		{
			Op:    SubDocOpDictSet,
			Flags: SubdocFlagXattrPath | SubdocFlagMkDirP,
			Path:  "xatest.test",
			Value: []byte("\"test value\""),
		},
		// TODO: Turn on Macro Expansion part of the xattr test
		/*{
			Op: SubDocOpDictSet,
			Flags: SubdocFlagXattrPath | SubdocFlagExpandMacros | SubdocFlagMkDirP,
			Path: "xatest.rev",
			Value: []byte("\"${Mutation.CAS}\""),
		},*/
		{
			Op:    SubDocOpDictSet,
			Flags: SubdocFlagNone,
			Path:  "x",
			Value: []byte("\"x value\""),
		},
	}
	agent.SubDocMutate([]byte("testXattr"), mutateOps, 0, 0, 0, func(res []SubDocResult, cas Cas, token MutationToken, err error) {
		s.Wrap(func() {
			if err != nil {
				s.Fatalf("Mutate operation failed: %v", err)
			}
			if cas == Cas(0) {
				s.Fatalf("Invalid cas received")
			}
		})
	})
	s.Wait(0)

	lookupOps := []SubDocOp{
		{
			Op:    SubDocOpGet,
			Flags: SubdocFlagXattrPath,
			Path:  "xatest",
		},
		{
			Op:    SubDocOpGet,
			Flags: SubdocFlagNone,
			Path:  "x",
		},
	}
	agent.SubDocLookup([]byte("testXattr"), lookupOps, 0, func(res []SubDocResult, cas Cas, err error) {
		s.Wrap(func() {
			if len(res) != 2 {
				s.Fatalf("Lookup operation wrong count")
			}
			if res[0].Err != nil {
				s.Fatalf("Lookup operation 1 failed: %v", res[0].Err)
			}
			if res[1].Err != nil {
				s.Fatalf("Lookup operation 2 failed: %v", res[1].Err)
			}

			/*
				xatest := fmt.Sprintf(`{"test":"test value","rev":"0x%016x"}`, cas)
				if !bytes.Equal(res[0].Value, []byte(xatest)) {
					s.Fatalf("Unexpected xatest value %s (doc) != %s (header)", res[0].Value, xatest)
				}
			*/
			if !bytes.Equal(res[0].Value, []byte(`{"test":"test value"}`)) {
				s.Fatalf("Unexpected xatest value %s", res[0].Value)
			}
			if !bytes.Equal(res[1].Value, []byte(`"x value"`)) {
				s.Fatalf("Unexpected document value %s", res[1].Value)
			}
		})
	})
	s.Wait(0)
}

func TestStats(t *testing.T) {
	agent, s := getAgentnSignaler(t)
	numServers := agent.routingInfo.Get().clientMux.NumPipelines()

	agent.Stats("", func(stats map[string]SingleServerStats) {
		s.Wrap(func() {
			if len(stats) != numServers {
				t.Fatalf("Didn't get all stats!")
			}
			numPerServer := 0
			for srv, curStats := range stats {
				if curStats.Error != nil {
					t.Fatalf("Got error %v in stats for %s", curStats.Error, srv)
				}
				if numPerServer == 0 {
					numPerServer = len(curStats.Stats)
				}
				if numPerServer != len(curStats.Stats) {
					t.Fatalf("Got different number of stats for %s. Got %d, expected %d", srv, len(curStats.Stats), numPerServer)
				}
			}
		})
	})
	s.Wait(0)
}

func TestGetHttpEps(t *testing.T) {
	agent := getAgent(t)

	// Relies on a 3.0.0+ server
	n1qlEpList := agent.N1qlEps()
	if len(n1qlEpList) == 0 {
		t.Fatalf("Failed to retrieve N1QL endpoint list")
	}

	mgmtEpList := agent.MgmtEps()
	if len(mgmtEpList) == 0 {
		t.Fatalf("Failed to retrieve N1QL endpoint list")
	}

	capiEpList := agent.CapiEps()
	if len(capiEpList) == 0 {
		t.Fatalf("Failed to retrieve N1QL endpoint list")
	}
}

func TestMemcachedBucket(t *testing.T) {
	// Ensure we can do upserts..
	agent := globalMemdAgent
	s := getSignaler(t)

	agent.Set([]byte("key"), []byte("value"), 0, 0, func(cas Cas, mt MutationToken, err error) {
		s.Wrap(func() {
			if err != nil {
				t.Fatalf("Got error for Set: %v", err)
			}
		})
	})
	s.Wait(0)

	agent.Get([]byte("key"), func(value []byte, flags uint32, cas Cas, err error) {
		s.Wrap(func() {
			if err != nil {
				t.Fatalf("Couldn't get back key: %v", err)
			}
			if string(value) != "value" {
				t.Fatalf("Got back wrong value!")
			}
		})
	})
	s.Wait(0)

	// Try to perform Observe: should fail since this isn't supported on Memcached buckets
	_, err := agent.Observe([]byte("key"), 0, func(ks KeyState, cas Cas, err error) {
		s.Wrap(func() {
			t.Fatalf("Scheduling should fail on memcached buckets!")
		})
	})

	if err != ErrNotSupported {
		t.Fatalf("Expected observe error for memcached bucket!")
	}

	// Try to use GETL, should also yield unsupported
	agent.GetAndLock([]byte("key"), 10, func(val []byte, flags uint32, cas Cas, err error) {
		s.Wrap(func() {
			if !IsErrorStatus(err, StatusNotSupported) &&
				!IsErrorStatus(err, StatusUnknownCommand) {
				t.Fatalf("GETL should fail on memcached buckets: %v", err)
			}
		})
	})
	s.Wait(0)
}

func TestMain(m *testing.M) {
	SetLogger(DefaultStdioLogger())
	flag.Parse()
	mpath, err := gojcbmock.GetMockPath()
	if err != nil {
		panic(err.Error())
	}

	globalMock, err = gojcbmock.NewMock(mpath, 4, 1, 64, []gojcbmock.BucketSpec{
		{Name: "default", Type: gojcbmock.BCouchbase},
		{Name: "memd", Type: gojcbmock.BMemcached},
	}...)

	if err != nil {
		panic(err.Error())
	}
	var memdAddrs []string
	for _, mcport := range globalMock.MemcachedPorts() {
		memdAddrs = append(memdAddrs, fmt.Sprintf("127.0.0.1:%d", mcport))
	}

	httpAddrs := []string{fmt.Sprintf("127.0.0.1:%d", globalMock.EntryPort)}

	agentConfig := &AgentConfig{
		MemdAddrs:            memdAddrs,
		HttpAddrs:            httpAddrs,
		TlsConfig:            nil,
		BucketName:           "default",
		Password:             "",
		AuthHandler:          saslAuthFn("default", ""),
		ConnectTimeout:       5 * time.Second,
		ServerConnectTimeout: 1 * time.Second,
		UseMutationTokens:    true,
		UseKvErrorMaps:       true,
		UseEnhancedErrors:    true,
	}

	globalAgent, err = CreateAgent(agentConfig)
	if err != nil {
		panic("Failed to connect to server")
	}

	memdAgentConfig := &AgentConfig{}
	*memdAgentConfig = *agentConfig
	memdAgentConfig.MemdAddrs = nil
	memdAgentConfig.BucketName = "memd"
	memdAgentConfig.AuthHandler = saslAuthFn("memd", "")
	globalMemdAgent, err = CreateAgent(memdAgentConfig)
	if err != nil {
		panic(fmt.Sprintf("Failed to connect to memcached bucket!: %v", err))
	}

	os.Exit(m.Run())
}
