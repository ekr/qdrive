package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
)

var shimFile string
var testCaseFile string
var verbose bool

type role uint8

const (
	roleClient = role(1)
	roleServer = role(2)
)

type endpoint struct {
	name   string
	role   role
	sock   *net.UDPConn
	addr   *net.UDPAddr
	cmd    *exec.Cmd
	out    *bufio.Reader
	err    *bufio.Reader
	status error
}

type failureReport struct {
	testCase string
	clErr    error
	srErr    error
	msg      string
}

type testStatus struct {
	ran       int
	succeeded int
	failed    int
	failures  []failureReport
}

// Global variable for results.
var status = testStatus{
	0,
	0,
	0,
	make([]failureReport, 0),
}

const kMaxUdpPacket = int(65535)

func debug(format string, args ...interface{}) {
	if verbose {
		fmt.Printf(format+"\n", args...)
	}
}

// Create an endpoint to run a client.
func newClientEndpoint(impl *implementation, role role, args []string) (*endpoint, error) {
	usock, err := net.ListenUDP("udp", nil)
	if err != nil {
		return nil, err
	}

	_, port, err := net.SplitHostPort(usock.LocalAddr().String())
	if err != nil {
		return nil, err
	}
	args = append(args, []string{"-addr", fmt.Sprintf("localhost:%s", port)}...)

	cmd := exec.Command(impl.Path, append(impl.Args, args...)...)
	ep := &endpoint{
		"client",
		role,
		usock,
		nil,
		cmd,
		nil,
		nil,
		nil,
	}

	err = ep.getOutputs()
	if err != nil {
		return nil, err
	}

	err = cmd.Start()
	if err != nil {
		return nil, err
	}

	return ep, nil
}

// Endpoint to run a server.
func newServerEndpoint(impl *implementation, role role, args []string) (*endpoint, error) {
	usock, err := net.ListenUDP("udp", nil)
	if err != nil {
		return nil, err
	}

	_, port, err := net.SplitHostPort(usock.LocalAddr().String())
	if err != nil {
		return nil, err
	}
	args = append(args, []string{"-addr", fmt.Sprintf("localhost:%s", port)}...)

	cmd := exec.Command(impl.Path, append(impl.Args, args...)...)

	ep := &endpoint{
		"server",
		role,
		usock,
		nil,
		cmd,
		nil,
		nil,
		nil,
	}

	err = ep.getOutputs()
	if err != nil {
		return nil, err
	}

	err = cmd.Start()
	if err != nil {
		return nil, err
	}
	sport, err := ep.out.ReadString('\n')
	if err != nil {
		return nil, err
	}
	debug("Read server port=%v", sport)
	sport = strings.TrimSpace(sport)
	ep.addr, err = net.ResolveUDPAddr("udp", fmt.Sprintf("localhost:%s", sport))
	if err != nil {
		return nil, err
	}

	return ep, nil
}

func (e *endpoint) getOutputs() error {
	tmp, err := e.cmd.StdoutPipe()
	if err != nil {
		return err
	}
	e.out = bufio.NewReader(tmp)

	tmp, err = e.cmd.StderrPipe()
	if err != nil {
		return err
	}
	e.err = bufio.NewReader(tmp)

	return nil
}

func shuttle1(from, to *endpoint) {
	buf := make([]byte, kMaxUdpPacket)

	for {
		n, addr, err := from.sock.ReadFromUDP(buf)
		if err != nil {
			continue
		}

		if from.addr == nil {
			from.addr = addr
		}

		debug("Read %v bytes writing to %v", n, to.addr)

		_, err = to.sock.WriteToUDP(buf[:n], to.addr)
		if err != nil {
			break
		}
	}
}

func readOutput(rdr *bufio.Reader, c chan *string) {
	for {
		str, err := rdr.ReadString('\n')
		if err != nil {
			c <- nil
			return
		}
		str = strings.TrimSpace(str)
		c <- &str
	}
}

func waitClose(name string, cmd *exec.Cmd, c chan error) {
	s := cmd.Wait()
	debug("Subprocess %s closed status=%v", name, s)
	c <- s

}

func runEndpoint(e *endpoint, c chan *string) {
	out := make(chan *string)
	err := make(chan *string)
	wait := make(chan error)

	go readOutput(e.out, out)
	go readOutput(e.err, err)
	go waitClose(e.name, e.cmd, wait)

	for {
		select {
		case r := <-out:
			c <- r
		case r := <-err:
			c <- r
		case r := <-wait:
			e.status = r
			c <- nil
		}
	}
}

func shuttle(cl, sr *endpoint) {
	debug("Shuttling")
	cchan := make(chan *string)
	schan := make(chan *string)
	crun := true
	srun := true

	go runEndpoint(cl, cchan)
	go runEndpoint(sr, schan)
	go shuttle1(cl, sr)
	go shuttle1(sr, cl)

	for crun || srun {
		select {
		case r := <-cchan:
			if r == nil {
				debug("Client exited")
				if cl.status != nil {
					return
				}
				crun = false
			} else {
				debug("Client: %v", *r)
			}

		case r := <-schan:
			if r == nil {
				debug("Server exited")
				if sr.status != nil {
					return
				}
				srun = false
			} else {
				debug("Server: %v", *r)
			}
		}
	}
}

func (t *testCase) finished(clErr, srErr error, msg string) {
	if clErr == nil && srErr == nil {
		status.succeeded++
		return
	}

	status.failed++
	status.failures = append(status.failures,
		failureReport{t.Name, clErr, srErr, msg})
	fmt.Printf("FAILED: %v\n", t.Name)
}

func (t *testCase) run(e *endpoints) error {
	debug("Running %s", t.Name)
	cl, err := newClientEndpoint(&e.Client, roleClient, t.ClientArgs)
	if err != nil {
		return err
	}

	sr, err := newServerEndpoint(&e.Server, roleServer, t.ServerArgs)
	if err != nil {
		return err
	}

	shuttle(cl, sr)

	t.finished(cl.status, sr.status, "Failed")

	return nil
}

type implementation struct {
	Path string
	Args []string
}

type endpoints struct {
	Client implementation
	Server implementation
}

type testCase struct {
	Name       string
	ClientArgs []string
	ServerArgs []string
}

type testCases struct {
	Cases []testCase
}

func reportResults() {
	fmt.Printf("Ran=%v Success=%v Failure=%v\n", status.ran, status.succeeded, status.failed)
}

func main() {
	flag.StringVar(&testCaseFile, "cases", "cases.json", "test cases file")
	flag.StringVar(&shimFile, "shims", "test.json", "config file")
	flag.BoolVar(&verbose, "verbose", false, "verbose debugging")
	flag.Parse()

	// Read the config file.
	var conf endpoints
	cfile, err := os.Open(shimFile)
	if err != nil {
		fmt.Println("Error opening: ", err)
		return
	}
	dec := json.NewDecoder(cfile)
	err = dec.Decode(&conf)
	if err != nil {
		fmt.Println("Error decoding: ", err)
		return
	}

	// Read the test cases file.
	var cases testCases
	tfile, err := os.Open(testCaseFile)
	if err != nil {
		fmt.Println("Error opening: ", err)
		return
	}
	dec = json.NewDecoder(tfile)
	err = dec.Decode(&cases)
	if err != nil {
		fmt.Println("Error decoding: ", err)
		return
	}

	debug("Test cases: %v", cases)
	for _, c := range cases.Cases {
		status.ran++

		err := c.run(&conf)
		if err != nil {
			fmt.Printf("Internal error: %v\n", err)
			return
		}
	}

	reportResults()
}
