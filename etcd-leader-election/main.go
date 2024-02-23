package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
	"golang.org/x/sync/errgroup"
)

type Server struct {
	sync.Mutex
	*http.Server
	addr       string
	leaderAddr string
	etcdCli    *clientv3.Client
}

func newHttpServer(s *Server) *http.Server {
	return &http.Server{
		Addr: s.addr,
		Handler: http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case "GET":
					w.Write(
						[]byte(
							fmt.Sprintf(
								"Hello, I(%s) am \n", s.addr,
							),
						),
					)
				case "POST":
					if s.IsLeader() {
						w.Write(
							[]byte(
								fmt.Sprintf(
									"Hello, I(%s) am the leader, "+
										"handling the POST request\n", s.addr,
								),
							),
						)
						return
					}
					// forward the request to the leader using host reverse proxy
					log.Printf("Forwarding request to leader: %s\n", s.leaderAddr)
					proxy := httputil.NewSingleHostReverseProxy(
						&url.URL{
							Scheme: "http",
							Host:   s.leaderAddr,
						},
					)
					r.Header.Set("X-Forwarded-For", r.RemoteAddr)
					proxy.ServeHTTP(w, r)
				default:
					log.Printf("Method not allowed: %s\n", r.Method)
					w.WriteHeader(http.StatusMethodNotAllowed)
				}

			},
		),
	}
}

func NewServer(addr string) (*Server, error) {
	server := &Server{
		addr: addr,
	}
	// create client
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{"localhost:2379"},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	server.etcdCli = cli
	server.Server = newHttpServer(server)
	return server, nil
}

func (s *Server) IsLeader() bool {
	s.Lock()
	defer s.Unlock()
	return s.addr == s.leaderAddr
}

func (svr *Server) Start() error {
	defer svr.etcdCli.Close()
	// create a sessions to elect a leader
	log.Printf("I(%s) am setting up the session\n", svr.addr)
	s, err := concurrency.NewSession(svr.etcdCli, concurrency.WithTTL(5))
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}

	// create an election on a key prefix
	e := concurrency.NewElection(s, "/my-election")
	ctx := context.Background()
	errGroup, ctx := errgroup.WithContext(ctx)
	reElection := make(chan struct{})

	// observe the leader
	errGroup.Go(
		func() error {
			respChan := e.Observe(ctx)
			log.Printf("I(%s) am observing the leader\n", svr.addr)
			for {
				select {
				case <-ctx.Done():
					log.Printf("I(%s) am NO LONGER the leader\n", svr.addr)
					return nil
				case resp, ok := <-respChan:
					if !ok {
						return errors.New("respChan is closed")
					}
					svr.Lock()
					wasLeader := svr.leaderAddr == svr.addr
					isLeader := wasLeader
					if len(resp.Kvs) > 0 {
						svr.leaderAddr = string(resp.Kvs[0].Value)
						log.Printf(
							"I(%s) am observing the leader: %s\n",
							svr.addr, svr.leaderAddr,
						)
						isLeader = svr.leaderAddr == svr.addr
					}
					if wasLeader != isLeader {
						// try to re-elect if the leader is lost
						reElection <- struct{}{}
					}
					svr.Unlock()
				}
			}
		},
	)

	errGroup.Go(func() error {
		log.Printf("I(%s) am trying to become the leader\n", svr.addr)
		for {
			if err := e.Campaign(ctx, svr.addr); err != nil {
				return err
			}
			<-reElection
		}
	})

	// stop the server if the session is ended
	errGroup.Go(
		func() error { // stop the server if the session is ended
			<-s.Done()
			log.Printf("I(%s) am NO LONGER the leader\n", svr.addr)
			if err := svr.Shutdown(context.Background()); err != nil {
				return fmt.Errorf("Server Shutdown Failed:%+s", err)
			}
			ctx.Done()
			log.Printf("Server stopped")
			return nil
		},
	)

	// start the server
	errGroup.Go(
		func() error {
			log.Printf("Servering on %s\n", svr.addr)
			if err := svr.ListenAndServe(); err != nil {
				log.Printf("Server failed to start: %v", err)
				return err
			}
			return nil
		},
	)

	// wait for the error group to finish
	err = errGroup.Wait()
	if err != nil {
		return err
	}
	return nil
}

func main() {
	port := flag.String("port", "8080", "port to listen on")
	flag.Parse()
	addr := fmt.Sprintf("localhost:%s", *port)

	server, err := NewServer(addr)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	log.Printf("Server is starting on %s\n", addr)
	if err := server.Start(); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
