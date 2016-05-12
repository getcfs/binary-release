package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"os"
	"os/user"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/gholt/brimtext"
	pb "github.com/pandemicsyn/syndicate/api/proto"
	"golang.org/x/net/context"
)

//SyndClient returns a client for interacting with a synd instance
type SyndClient struct {
	conn   *grpc.ClientConn
	client pb.SyndicateClient
}

//NewSyndicateClient returns a client for interacting with the syndicate
func NewSyndicateClient() (*SyndClient, error) {
	var err error
	var opts []grpc.DialOption
	var creds credentials.TransportAuthenticator
	creds = credentials.NewTLS(&tls.Config{
		InsecureSkipVerify: true,
	})
	opts = append(opts, grpc.WithTransportCredentials(creds))
	s := SyndClient{}
	if *groupMode {
		s.conn, err = grpc.Dial("127.0.0.1:8444", opts...)
		if err != nil {
			return &SyndClient{}, fmt.Errorf("Failed to dial ring server for config: %v", err)
		}
	} else {
		s.conn, err = grpc.Dial(*syndicateAddr, opts...)
		if err != nil {
			return &SyndClient{}, fmt.Errorf("Failed to dial ring server for config: %v", err)
		}
	}
	s.client = pb.NewSyndicateClient(s.conn)
	return &s, nil
}

func (s *SyndClient) printVersionCmd() error {
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	status, err := s.client.GetVersion(ctx, &pb.EmptyMsg{})
	if err != nil {
		return err
	}
	fmt.Println("Version:", status.Version)
	return nil
}

func (s *SyndClient) rmNodeCmd(id uint64) error {
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	c, err := s.client.RemoveNode(ctx, &pb.Node{Id: id})
	if err != nil {
		return err
	}
	report := [][]string{
		[]string{"Status:", fmt.Sprintf("%v", c.Status)},
		[]string{"Version:", fmt.Sprintf("%v", c.Version)},
	}
	fmt.Print(brimtext.Align(report, nil))
	return nil
}

func (s *SyndClient) setActiveCmd(id uint64, active bool) error {
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	c, err := s.client.SetActive(ctx, &pb.Node{Id: id, Active: active})
	if err != nil {
		return err
	}
	report := [][]string{
		[]string{"Status:", fmt.Sprintf("%v", c.Status)},
		[]string{"Version:", fmt.Sprintf("%v", c.Version)},
	}
	fmt.Print(brimtext.Align(report, nil))
	return nil
}

func (s *SyndClient) setCapacityCmd(id uint64, capacity uint32) error {
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	c, err := s.client.SetCapacity(ctx, &pb.Node{Id: id, Capacity: capacity})
	if err != nil {
		return err
	}
	report := [][]string{
		[]string{"Status:", fmt.Sprintf("%v", c.Status)},
		[]string{"Version:", fmt.Sprintf("%v", c.Version)},
	}
	fmt.Print(brimtext.Align(report, nil))
	return nil
}

func (s *SyndClient) setAddressCmd(id uint64, addrs []string) error {
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	c, err := s.client.ReplaceAddresses(ctx, &pb.Node{Id: id, Addresses: addrs})
	if err != nil {
		return err
	}
	report := [][]string{
		[]string{"Status:", fmt.Sprintf("%v", c.Status)},
		[]string{"Version:", fmt.Sprintf("%v", c.Version)},
	}
	fmt.Print(brimtext.Align(report, nil))
	return nil
}

func (s *SyndClient) setTierCmd(id uint64, tiers []string) error {
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	c, err := s.client.ReplaceTiers(ctx, &pb.Node{Id: id, Tiers: tiers})
	if err != nil {
		return err
	}
	report := [][]string{
		[]string{"Status:", fmt.Sprintf("%v", c.Status)},
		[]string{"Version:", fmt.Sprintf("%v", c.Version)},
	}
	fmt.Print(brimtext.Align(report, nil))
	return nil
}

func (s *SyndClient) printNodeConfigCmd(id uint64) error {
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	c, err := s.client.GetNodeConfig(ctx, &pb.Node{Id: id})
	if err != nil {
		return err
	}
	report := [][]string{
		[]string{"Status:", fmt.Sprintf("%v", c.Status.Status)},
		[]string{"Version:", fmt.Sprintf("%v", c.Status.Version)},
		[]string{"Conf:", string(c.Conf.Conf)},
	}
	fmt.Print(brimtext.Align(report, nil))
	return nil
}

func (s *SyndClient) printConfigCmd() error {
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	c, err := s.client.GetGlobalConfig(ctx, &pb.EmptyMsg{})
	if err != nil {
		return err
	}
	report := [][]string{
		[]string{"Status:", fmt.Sprintf("%v", c.Status.Status)},
		[]string{"Version:", fmt.Sprintf("%v", c.Status.Version)},
		[]string{"Conf:", string(c.Conf.Conf)},
	}
	fmt.Print(brimtext.Align(report, nil))
	return nil
}

// SetConfig sets the global ring config to the provided bytes, and indicates
// whether the config change should trigger a restart.
func (s *SyndClient) SetConfig(config []byte, restart bool) (err error) {
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	confMsg := &pb.Conf{
		Conf:            config,
		RestartRequired: restart,
	}
	status, err := s.client.SetConf(ctx, confMsg)
	if err != nil {
		return err
	}
	report := [][]string{
		[]string{"Status:", fmt.Sprintf("%v", status.Status)},
		[]string{"Version:", fmt.Sprintf("%v", status.Version)},
	}
	fmt.Print(brimtext.Align(report, nil))
	return nil
}

// SearchNodes uses a provide pb.Node to search for matching nodes in the active ring
func (s *SyndClient) SearchNodes(args []string) (err error) {
	filter := &pb.Node{}
	for _, arg := range args {
		sarg := strings.SplitN(arg, "=", 2)
		if len(sarg) != 2 {
			return fmt.Errorf(`invalid expression %#v; needs "="`, arg)
		}
		if sarg[0] == "" {
			return fmt.Errorf(`invalid expression %#v; nothing was left of "="`, arg)
		}
		if sarg[1] == "" {
			return fmt.Errorf(`invalid expression %#v; nothing was right of "="`, arg)
		}
		switch sarg[0] {
		case "id":
			filter.Id, err = strconv.ParseUint(sarg[1], 0, 64)
			if err != nil {
				return err
			}
		case "meta":
			filter.Meta = sarg[1]
		default:
			if strings.HasPrefix(sarg[0], "tier") {
				var tiers []string
				level, err := strconv.Atoi(sarg[0][4:])
				if err != nil {
					return fmt.Errorf("invalid expression %#v; %#v doesn't specify a number", arg, sarg[0][4:])
				}
				if level < 0 {
					return fmt.Errorf("invalid expression %#v; minimum level is 0", arg)
				}
				if len(tiers) <= level {
					t := make([]string, level+1)
					copy(t, tiers)
					tiers = t
				}
				tiers[level] = sarg[1]
				filter.Tiers = tiers
			} else if strings.HasPrefix(sarg[0], "address") {
				var addresses []string
				index, err := strconv.Atoi(sarg[0][7:])
				if err != nil {
					return fmt.Errorf("invalid expression %#v; %#v doesn't specify a number", arg, sarg[0][4:])
				}
				if index < 0 {
					return fmt.Errorf("invalid expression %#v; minimum index is 0", arg)
				}
				if len(addresses) <= index {
					a := make([]string, index+1)
					copy(a, addresses)
					addresses = a
				}
				addresses[index] = sarg[1]
				filter.Addresses = addresses
			} else {
				return fmt.Errorf("unknown k/v combo: %s=%s", sarg[0], sarg[1])
			}
		}
	}
	fmt.Printf("Searching for: %#v\n", filter)
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	res, err := s.client.SearchNodes(ctx, filter)
	if err != nil {
		return err
	}
	if len(res.Nodes) == 0 {
		return fmt.Errorf("No results found")
	}
	for i, n := range res.Nodes {
		fmt.Println("# result", i)
		printNode(n)
	}

	return nil
}

//WatchRing prints out ring versions as ring changes occur
func (s *SyndClient) WatchRing() error {
	ctx := context.Background()
	hname, _ := os.Hostname()
	user, _ := user.Current()
	sid := pb.SubscriberID{Id: fmt.Sprintf("%s:%s-sc", hname, user.Name)}
	stream, err := s.client.GetRingStream(ctx, &sid)
	if err != nil {
		return err
	}
	for {
		ring, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		fmt.Println(ring.Version)
	}
	return nil
}
