package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"os/user"
	"time"

	"strconv"
	"strings"

	"github.com/gholt/brimtext"
	"github.com/gholt/ring"
	cc "github.com/pandemicsyn/cmdctrl/api"
	pb "github.com/pandemicsyn/syndicate/api/proto"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

var (
	syndicateAddr    = flag.String("addr", "127.0.0.1:8443", "syndicate host to connect too")
	printVersionInfo = flag.Bool("version", false, "print version/build info")
)

var syndicateClientVersion string
var ringVersion string
var buildDate string
var goVersion string

type SyndClient struct {
	conn   *grpc.ClientConn
	client pb.SyndicateClient
}

type CmdCtrlClient struct {
	conn   *grpc.ClientConn
	client cc.CmdCtrlClient
}

func printNode(n *pb.Node) {
	report := [][]string{
		[]string{"ID:", fmt.Sprintf("%d", n.Id)},
		[]string{"Active:", fmt.Sprintf("%v", n.Active)},
		[]string{"Capacity:", fmt.Sprintf("%d", n.Capacity)},
		[]string{"Tiers:", strings.Join(n.Tiers, "\n")},
		[]string{"Addresses:", strings.Join(n.Addresses, "\n")},
		[]string{"Meta:", n.Meta},
		[]string{"Conf:", string(n.Conf)},
	}
	fmt.Print(brimtext.Align(report, nil))
}

func NewCmdCtrlClient(address string) (*CmdCtrlClient, error) {
	var err error
	var opts []grpc.DialOption
	var creds credentials.TransportAuthenticator
	creds = credentials.NewTLS(&tls.Config{
		InsecureSkipVerify: true,
	})
	opts = append(opts, grpc.WithTransportCredentials(creds))
	s := CmdCtrlClient{}
	s.conn, err = grpc.Dial(address, opts...)
	if err != nil {
		return &CmdCtrlClient{}, fmt.Errorf("Failed to dial ring server for config: %v", err)
	}
	s.client = cc.NewCmdCtrlClient(s.conn)
	return &s, nil

}

func New() (*SyndClient, error) {
	var err error
	var opts []grpc.DialOption
	var creds credentials.TransportAuthenticator
	creds = credentials.NewTLS(&tls.Config{
		InsecureSkipVerify: true,
	})
	opts = append(opts, grpc.WithTransportCredentials(creds))
	s := SyndClient{}
	s.conn, err = grpc.Dial(*syndicateAddr, opts...)
	if err != nil {
		return &SyndClient{}, fmt.Errorf("Failed to dial ring server for config: %v", err)
	}
	s.client = pb.NewSyndicateClient(s.conn)
	return &s, nil
}

func helpCmd() error {
	u, _ := user.Current()
	return fmt.Errorf(`I'm sorry %s, I'm afraid I can't do that. Valid commands are:

start <cmdctrladdress> #attempts to start the remote nodes backend
stop <cmdctrladdress> #attempts to stop the remote nodes backend
restart <cmdctrladdress> #attempts to restart the remote nodes backend
exit <cmdctrladdress> #attempts to exit the remote node
version			#print version
config          #print ring config
config <nodeid> #uses uint64 id
search			#lists all
search id=<nodeid>
search meta=<metastring>
search tier=<string> or search tierX=<string>
search address=<string> or search addressX=<string>
search any of the above K/V combos
rm <nodeid>
active <nodeid> true|false
capacity <nodeid> <uint32>
addrs <nodeid> 1.1.1.1,2.2.2.2,...
tiers <nodeid> SomeTier,SomeTier2,...
set config=./path/to/config
`, u.Username)
}

func main() {
	flag.Parse()
	if *printVersionInfo {
		fmt.Println("syndicate-client:", syndicateClientVersion)
		fmt.Println("ring version:", ringVersion)
		fmt.Println("build date:", buildDate)
		fmt.Println("go version:", goVersion)
		return
	}
	s, err := New()
	if err != nil {
		panic(err)
	}
	fmt.Println(flag.Args())
	if err := s.mainEntry(flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func (s *SyndClient) mainEntry(args []string) error {
	if len(args) == 0 || args[0] == "help" {
		return helpCmd()
	}
	switch args[0] {
	case "start":
		if len(args) != 2 {
			return helpCmd()
		}
		c, err := NewCmdCtrlClient(args[1])
		if err != nil {
			return err
		}
		return c.startNodeCmd()
	case "restart":
		if len(args) != 2 {
			return helpCmd()
		}
		c, err := NewCmdCtrlClient(args[1])
		if err != nil {
			return err
		}
		return c.restartNodeCmd()
	case "stop":
		if len(args) != 2 {
			return helpCmd()
		}
		c, err := NewCmdCtrlClient(args[1])
		if err != nil {
			return err
		}
		return c.stopNodeCmd()
	case "exit":
		if len(args) != 2 {
			return helpCmd()
		}
		c, err := NewCmdCtrlClient(args[1])
		if err != nil {
			return err
		}
		return c.exitNodeCmd()
	case "stats":
		if len(args) != 2 {
			return helpCmd()
		}
		c, err := NewCmdCtrlClient(args[1])
		if err != nil {
			return err
		}
		return c.statsNodeCmd()
	case "ringupdate":
		if len(args) != 3 {
			return helpCmd()
		}
		c, err := NewCmdCtrlClient(args[1])
		if err != nil {
			return err
		}
		return c.ringUpdateNodeCmd(args[2])
	case "version":
		return s.printVersionCmd()
	case "config":
		if len(args) == 1 {
			return s.printConfigCmd()
		}
		if len(args) == 2 {
			id, err := strconv.ParseUint(args[1], 0, 64)
			if err != nil {
				return err
			}
			return s.printNodeConfigCmd(id)
		}
	case "search":
		return s.SearchNodes(args[1:])
	case "rm":
		if len(args) == 2 {
			id, err := strconv.ParseUint(args[1], 0, 64)
			if err != nil {
				return err
			}
			return s.rmNodeCmd(id)
		}
	case "active":
		if len(args) == 3 {
			var active bool
			id, err := strconv.ParseUint(args[1], 0, 64)
			if err != nil {
				return err
			}
			if args[2] != "false" && args[2] != "true" {
				return fmt.Errorf("active must be 'true' or 'false'")
			}
			if args[2] == "true" {
				active = true
			} else {
				active = false
			}
			return s.setActiveCmd(id, active)
		}
	case "capacity":
		if len(args) == 3 {
			id, err := strconv.ParseUint(args[1], 0, 64)
			if err != nil {
				return err
			}
			c, err := strconv.Atoi(args[2])
			if err != nil {
				return fmt.Errorf("invalid expression %#v; %s", args[1], err)
			}
			if c < 0 {
				return fmt.Errorf("invalid expression %#v; min is 0", args[1])
			}
			if c > math.MaxUint32 {
				return fmt.Errorf("invalid expression %#v; max is %d", args[1], math.MaxUint32)
			}
			capacity := uint32(c)
			return s.setCapacityCmd(id, capacity)
		}
	case "tiers":
		if len(args) == 3 {
			id, err := strconv.ParseUint(args[1], 0, 64)
			if err != nil {
				return err
			}
			tiers := strings.Split(args[2], ",")
			return s.setTierCmd(id, tiers)
		}
	case "addrs":
		if len(args) == 3 {
			id, err := strconv.ParseUint(args[1], 0, 64)
			if err != nil {
				return err
			}
			addrs := strings.Split(args[2], ",")
			return s.setAddressCmd(id, addrs)
		}
	case "set":
		for _, arg := range args[1:] {
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
			case "config":
				conf, err := ioutil.ReadFile(sarg[1])
				if err != nil {
					return fmt.Errorf("Error reading config file: %v", err)
				}
				s.SetConfig(conf, false)
			}
		}
		return nil
	}
	return helpCmd()
}

func (s *CmdCtrlClient) startNodeCmd() error {
	ctx, _ := context.WithTimeout(context.Background(), 60*time.Second)
	status, err := s.client.Start(ctx, &cc.EmptyMsg{})
	if err != nil {
		return err
	}
	fmt.Println("Started:", status.Status, " Msg:", status.Msg)
	return nil
}

func (s *CmdCtrlClient) restartNodeCmd() error {
	ctx, _ := context.WithTimeout(context.Background(), 60*time.Second)
	status, err := s.client.Restart(ctx, &cc.EmptyMsg{})
	if err != nil {
		return err
	}
	fmt.Println("Restarted:", status.Status, " Msg:", status.Msg)
	return nil
}

func (s *CmdCtrlClient) stopNodeCmd() error {
	ctx, _ := context.WithTimeout(context.Background(), 60*time.Second)
	status, err := s.client.Stop(ctx, &cc.EmptyMsg{})
	if err != nil {
		return err
	}
	fmt.Println("Stopped:", status.Status, " Msg:", status.Msg)
	return nil
}

func (s *CmdCtrlClient) exitNodeCmd() error {
	ctx, _ := context.WithTimeout(context.Background(), 60*time.Second)
	status, err := s.client.Exit(ctx, &cc.EmptyMsg{})
	if err != nil {
		return err
	}
	fmt.Println("Stopped:", status.Status, " Msg:", status.Msg)
	return nil
}

func (s *CmdCtrlClient) statsNodeCmd() error {
	ctx, _ := context.WithTimeout(context.Background(), 60*time.Second)
	status, err := s.client.Stats(ctx, &cc.EmptyMsg{})
	if err != nil {
		return err
	}
	fmt.Printf("%s\n", status.Statsjson)
	return nil
}

func (s *CmdCtrlClient) ringUpdateNodeCmd(filename string) error {
	ctx, _ := context.WithTimeout(context.Background(), 60*time.Second)
	r, _, err := ring.RingOrBuilder(filename)
	if err != nil {
		return err
	}
	if r == nil {
		return fmt.Errorf("Provided builder file rather than ring file")
	}
	ru := &cc.Ring{}
	ru.Version = r.Version()
	ru.Ring, err = ioutil.ReadFile(filename)
	if err != nil {
		return err
	}
	status, err := s.client.RingUpdate(ctx, ru)
	if err != nil {
		return err
	}
	if status.Newversion != ru.Version {
		return fmt.Errorf("Ring update seems to have failed. Expected: %d, but remote host reports: %d\n", ru.Version, status.Newversion)
	}
	fmt.Println("Remote version is now", status.Newversion)
	return nil
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
