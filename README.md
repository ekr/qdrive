# qdrive -- an interop harness for QUIC implementations.

qdrive is an interop harness to let you test two QUIC stacks.
qdrive opens up a pair of UDP sockets, one for each side to
talk to, and then shuttles data in between them until either

- One program has exited with failure (non-zero)
- Both programs have exited with success (zero)

To use it, an implementation needs to provide a command line
"shim" program that will do QUIC and and then exit with success
or failure. 


## Running

To run qdrive, just do:

    go run qdrive -shims <shim configure file> -cases <test cases file>
    

## Shim Interface

The shim is run with the following arguments which control
its operation]

    -addr [the address to talk to qdrive on]

Without other control arguments (coming later), the expectation
is that each shim will do a QUIC handshake and then exit 0 with
success.

To make life easier, a shim acting as a server must print
out its port on stdout when it starts. This lets qdrive
know where to send packets.



## Configuring Shims

The two shims to use are specified with a JSON file in the
following format:

    {
        "Client" : {
            "Path" : "go",
            "Args" : ["run", "github.com/ekr/minq/bin/shim/main.go" ]
        },
        "Server" : {
            "Path" : "go",
            "Args" : ["run", "github.com/ekr/minq/bin/shim/main.go", "-server" ]
        }
    }

The |Path| and |Args| fields have the expected value. Note that because
you can provide extra args, you can have one shim be both client and
server as shown here, or have two separate shims.


## Configuring Test Cases

Test cases are also configured with JSON, as shown below:

    {
        "Cases" : [
            {
                "Name" : "basic",
                "ClientArgs": [],
                "ServerArgs": []
            }
            ]
    }

The fields have the expected meanings.




