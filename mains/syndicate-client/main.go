package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"os/user"

	"strconv"
	"strings"

	"github.com/gholt/brimtext"
	pb "github.com/pandemicsyn/syndicate/api/proto"
)

var (
	syndicateAddr    = flag.String("addr", "127.0.0.1:8443", "syndicate host to connect too")
	groupMode        = flag.Bool("group", false, "use default groupstore addr instead")
	printVersionInfo = flag.Bool("version", false, "print version/build info")
)

var syndicateClientVersion string
var ringVersion string
var buildDate string
var goVersion string

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

func helpCmd() error {
	u, _ := user.Current()
	return fmt.Errorf(`I'm sorry %s, I'm afraid I can't do that. Valid commands are:

start <cmdctrladdress> #attempts to start the remote nodes backend
stop <cmdctrladdress> #attempts to stop the remote nodes backend
restart <cmdctrladdress> #attempts to restart the remote nodes backend
exit <cmdctrladdress> #attempts to exit the remote node
upgrade <cmdctrladdress> <github version string> #asks the node to upgrade itself
softwareversion  <cmdctrladdress> #gets the currently running version from the node
version			#print version
config          #print ring config
config <nodeid> #uses uint64 id
search			#lists all
search id=<nodeid>
search meta=<metastring>
search tier=<string> or search tierX=<string>
search address=<string> or search addressX=<string>
search any of the above K/V combos
watch ring
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
	s, err := NewSyndicateClient()
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
	case "upgrade":
		if len(args) != 3 {
			return helpCmd()
		}
		c, err := NewCmdCtrlClient(args[1])
		if err != nil {
			return err
		}
		return c.upgradeNodeCmd(args[2])
	case "softwareversion":
		if len(args) != 2 {
			return helpCmd()
		}
		c, err := NewCmdCtrlClient(args[1])
		if err != nil {
			return err
		}
		return c.getSoftwareVersionCmd()
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
	case "watch":
		return s.WatchRing()
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
