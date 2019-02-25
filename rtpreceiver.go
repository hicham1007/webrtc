package webrtc

import (
	"fmt"
	"sync"

	"github.com/pions/rtcp"
)

// RTPReceiver allows an application to inspect the receipt of a Track
type RTPReceiver struct {
	kind      RTPCodecType
	transport *DTLSTransport

	track *Track

	closed, received chan interface{}
	mu               sync.RWMutex

	rtpReadStream, rtcpReadStream *lossyReadCloser

	// A reference to the associated api object
	api *API
}

// NewRTPReceiver constructs a new RTPReceiver
func (api *API) NewRTPReceiver(kind RTPCodecType, transport *DTLSTransport) (*RTPReceiver, error) {
	if transport == nil {
		return nil, fmt.Errorf("DTLSTransport must not be nil")
	}

	return &RTPReceiver{
		kind:      kind,
		transport: transport,
		api:       api,
		closed:    make(chan interface{}),
		received:  make(chan interface{}),
	}, nil
}

// Track returns the RTCRtpTransceiver track
func (r *RTPReceiver) Track() *Track {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.track
}

// Receive initialize the track and starts all the transports
func (r *RTPReceiver) Receive(parameters RTPReceiveParameters) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	select {
	case <-r.received:
		return fmt.Errorf("Receive has already been called")
	default:
	}
	close(r.received)

	r.track = &Track{
		kind:     r.kind,
		ssrc:     parameters.encodings.SSRC,
		receiver: r,
	}

	srtpSession, err := r.transport.getSRTPSession()
	if err != nil {
		return err
	}

	srtpReadStream, err := srtpSession.OpenReadStream(parameters.encodings.SSRC)
	if err != nil {
		return err
	}

	srtcpSession, err := r.transport.getSRTCPSession()
	if err != nil {
		return err
	}

	srtcpReadStream, err := srtcpSession.OpenReadStream(parameters.encodings.SSRC)
	if err != nil {
		return err
	}

	r.rtpReadStream = newLossyReadCloser(srtpReadStream)
	r.rtcpReadStream = newLossyReadCloser(srtcpReadStream)
	return nil
}

// Read reads incoming RTCP for this RTPReceiver
func (r *RTPReceiver) Read(b []byte) (n int, err error) {
	select {
	case <-r.closed:
		return 0, fmt.Errorf("RTPSender has been stopped")
	case <-r.received:
		return r.rtcpReadStream.read(b)
	}
}

// ReadRTCP is a convenience method that wraps Read and unmarshals for you
func (r *RTPReceiver) ReadRTCP(b []byte) (rtcp.Packet, error) {
	i, err := r.Read(b)
	if err != nil {
		return nil, err
	}

	pkt, _, err := rtcp.Unmarshal(b[:i])
	return pkt, err
}

// Stop irreversibly stops the RTPReceiver
func (r *RTPReceiver) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	select {
	case <-r.closed:
		return nil
	default:
	}

	select {
	case <-r.received:
		if err := r.rtcpReadStream.close(); err != nil {
			return err
		}
		if err := r.rtpReadStream.close(); err != nil {
			return err
		}
	default:
	}

	close(r.closed)
	return nil
}

// readRTP should only be called by a track, this only exists so we can keep state in one place
func (r *RTPReceiver) readRTP(b []byte) (n int, err error) {
	select {
	case <-r.closed:
		return 0, fmt.Errorf("RTPSender has been stopped")
	case <-r.received:
		return r.rtpReadStream.read(b)
	}
}
