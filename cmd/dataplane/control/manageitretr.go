package main

import (
	"github.com/google/gopacket/pfring"
	"github.com/zededa/go-provision/dataplane/etr"
	"github.com/zededa/go-provision/dataplane/itr"
	"github.com/zededa/go-provision/types"
	"log"
	"syscall"
)

type ThreadEntry struct {
	channel chan bool
	ring    *pfring.Ring
}

var threadTable map[string]ThreadEntry
var etrRunStatus types.EtrRunStatus

func InitEtrRunStatus() {
	etrRunStatus = types.EtrRunStatus{-1, nil, nil, -1, -1}
}

func InitThreadTable() {
	threadTable = make(map[string]ThreadEntry)
}

func DumpThreadTable() {
	for name, _ := range threadTable {
		log.Println(name)
	}
}

// Find the difference between running ITR threads and the threads
// that need to be running according to new configuration.
//
// Kill the ITR threads that are no longer needed and create
// newly required threads.
func ManageItrThreads(interfaces Interfaces) {
	tmpMap := make(map[string]bool)

	// Build a map of threads needed according to new configuration
	for _, iface := range interfaces.Interfaces {
		tmpMap[iface.Interface] = true
	}

	// Kill ITR threads that are not needed with new configuration
	//for name, channel := range threadTable {
	for name, entry := range threadTable {
		// Check if this thread is needed with new configuration and send
		// a kill signal if not.
		if _, ok := tmpMap[name]; !ok {
			// This thread has to die, break the bad news to it
			log.Println("Sending kill signal to", name)
			entry.channel <- true

			// XXX
			// ITR threads use pf_ring for packet capture.
			// pf_ring packet read calls are blocking. If a thread is blocked
			// and there are no packets coming in from the corresponding interface,
			// it can never get unblocked and process messages from kill channel.
			//
			// We delete the pf_ring socket for now, so that the ITR thread blocking
			// calls returns with error.
			//
			// We'll retain the kill channel mechanism for future optimizations.
			close(entry.channel)
			entry.ring.Disable()
			entry.ring.Close()
			delete(threadTable, name)
		}
	}

	// Create new threads that do not already exist
	for name, _ := range tmpMap {
		if _, ok := threadTable[name]; !ok {
			// This ITR thread has to be given birth to. Find a mom!!
			killChannel := make(chan bool, 1)

			// Start the go thread here
			ring := itr.SetupPacketCapture(name, 65536)
			log.Println("Creating new ITR thread for", name)
			threadTable[name] = ThreadEntry{
				channel: killChannel,
				ring:    ring,
			}
			go itr.StartItrThread(name, ring, killChannel, puntChannel)
		}
	}
}

func ManageETRThread(port int) {
	// return if the ephemeral port that we currently use is same
	if etrRunStatus.EphPort == port {
		return
	}
	// Destroy the previous ETR run state
	if etrRunStatus.Ring != nil {
		etrRunStatus.Ring.Disable()
		etrRunStatus.Ring.Close()
	}
	if etrRunStatus.UdpConn != nil {
		etrRunStatus.UdpConn.Close()
	}
	if etrRunStatus.RingFD != -1 {
		syscall.Close(etrRunStatus.RingFD)
	}
	if etrRunStatus.UdpFD != -1 {
		syscall.Close(etrRunStatus.UdpFD)
	}
	udpConn, ring, fd1, fd2 := etr.StartETR(port)
	etrRunStatus.UdpConn = udpConn
	etrRunStatus.Ring = ring
	etrRunStatus.UdpFD = fd1
	etrRunStatus.RingFD = fd2
	etrRunStatus.EphPort = port
}
