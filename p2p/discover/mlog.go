package discover

import (
	"github.com/openether/ethcore/logger"
)

var mlogDiscover = logger.MLogRegisterAvailable("discover", mLogLines)

// mLogLines is a private slice of all available mlog LINES.
// May be used for automatic mlog docmentation generator, or
// for API usage/display/documentation otherwise.
var mLogLines = []*logger.MLogT{
	mlogPingHandleFrom,
	mlogPingSendTo,
	mlogPongHandleFrom,
	mlogPongSendTo,
	mlogFindNodeHandleFrom,
	mlogFindNodeSendTo,
	mlogNeighborsHandleFrom,
	mlogNeighborsSendTo,
}

// Collect and document available mlog lines.

// PING
// mlogPingHandleFrom is called once for each ping request from a node FROM
var mlogPingHandleFrom = &logger.MLogT{
	Description: "Called once for each received PING request from peer FROM.",
	Receiver:    "PING",
	Verb:        "HANDLE",
	Subject:     "FROM",
	Details: []logger.MLogDetailT{
		{Owner: "FROM", Key: "UDP_ADDRESS", Value: "STRING"},
		{Owner: "FROM", Key: "ID", Value: "STRING"},
		{Owner: "PING", Key: "BYTES_TRANSFERRED", Value: "INT"},
	},
}

var mlogPingSendTo = &logger.MLogT{
	Description: "Called once for each outgoing PING request to peer TO.",
	Receiver:    "PING",
	Verb:        "SEND",
	Subject:     "TO",
	Details: []logger.MLogDetailT{
		{Owner: "TO", Key: "UDP_ADDRESS", Value: "STRING"},
		{Owner: "PING", Key: "BYTES_TRANSFERRED", Value: "INT"},
	},
}

// PONG
// mlogPongHandleFrom is called once for each pong request from a node FROM
var mlogPongHandleFrom = &logger.MLogT{
	Description: "Called once for each received PONG request from peer FROM.",
	Receiver:    "PONG",
	Verb:        "HANDLE",
	Subject:     "FROM",
	Details: []logger.MLogDetailT{
		{Owner: "FROM", Key: "UDP_ADDRESS", Value: "STRING"},
		{Owner: "FROM", Key: "ID", Value: "STRING"},
		{Owner: "PONG", Key: "BYTES_TRANSFERRED", Value: "INT"},
	},
}

// mlogPingHandleFrom is called once for each ping request from a node FROM
var mlogPongSendTo = &logger.MLogT{
	Description: "Called once for each outgoing PONG request to peer TO.",
	Receiver:    "PONG",
	Verb:        "SEND",
	Subject:     "TO",
	Details: []logger.MLogDetailT{
		{Owner: "TO", Key: "UDP_ADDRESS", Value: "STRING"},
		{Owner: "PONG", Key: "BYTES_TRANSFERRED", Value: "INT"},
	},
}

// FINDNODE
// mlogFindNodeHandleFrom is called once for each findnode request from a node FROM
var mlogFindNodeHandleFrom = &logger.MLogT{
	Description: "Called once for each received FIND_NODE request from peer FROM.",
	Receiver:    "FINDNODE",
	Verb:        "HANDLE",
	Subject:     "FROM",
	Details: []logger.MLogDetailT{
		{Owner: "FROM", Key: "UDP_ADDRESS", Value: "STRING"},
		{Owner: "FROM", Key: "ID", Value: "STRING"},
		{Owner: "FINDNODE", Key: "BYTES_TRANSFERRED", Value: "INT"},
	},
}

// mlogFindNodeHandleFrom is called once for each findnode request from a node FROM
var mlogFindNodeSendTo = &logger.MLogT{
	Description: "Called once for each received FIND_NODE request from peer FROM.",
	Receiver:    "FINDNODE",
	Verb:        "SEND",
	Subject:     "TO",
	Details: []logger.MLogDetailT{
		{Owner: "TO", Key: "UDP_ADDRESS", Value: "STRING"},
		{Owner: "FINDNODE", Key: "BYTES_TRANSFERRED", Value: "INT"},
	},
}

// NEIGHBORS
// mlogNeighborsHandleFrom is called once for each neighbors request from a node FROM
var mlogNeighborsHandleFrom = &logger.MLogT{
	Description: `Called once for each received NEIGHBORS request from peer FROM.`,
	Receiver:    "NEIGHBORS",
	Verb:        "HANDLE",
	Subject:     "FROM",
	Details: []logger.MLogDetailT{
		{Owner: "FROM", Key: "UDP_ADDRESS", Value: "STRING"},
		{Owner: "FROM", Key: "ID", Value: "STRING"},
		{Owner: "NEIGHBORS", Key: "BYTES_TRANSFERRED", Value: "INT"},
	},
}

// mlogFindNodeSendNeighbors is called once for each sent NEIGHBORS request
var mlogNeighborsSendTo = &logger.MLogT{
	Description: `Called once for each outgoing NEIGHBORS request to peer TO.`,
	Receiver:    "NEIGHBORS",
	Verb:        "SEND",
	Subject:     "TO",
	Details: []logger.MLogDetailT{
		{Owner: "TO", Key: "UDP_ADDRESS", Value: "STRING"},
		{Owner: "NEIGHBORS", Key: "BYTES_TRANSFERRED", Value: "INT"},
	},
}
