package cursor

import (
	"encoding/binary"
	"encoding/json"
)

const connectEndStreamFlag = 0b00000010

func frameConnect(payload []byte, flags byte) []byte {
	out := make([]byte, 5+len(payload))
	out[0] = flags
	binary.BigEndian.PutUint32(out[1:5], uint32(len(payload)))
	copy(out[5:], payload)
	return out
}

type frameParser struct {
	pending []byte
}

func (p *frameParser) Push(incoming []byte, onMessage, onEnd func([]byte)) {
	p.pending = append(p.pending, incoming...)
	for len(p.pending) >= 5 {
		flags := p.pending[0]
		msgLen := binary.BigEndian.Uint32(p.pending[1:5])
		if uint32(len(p.pending)) < 5+msgLen {
			return
		}
		msg := append([]byte(nil), p.pending[5:5+msgLen]...)
		p.pending = p.pending[5+msgLen:]
		if flags&connectEndStreamFlag != 0 {
			if onEnd != nil {
				onEnd(msg)
			}
		} else if onMessage != nil {
			onMessage(msg)
		}
	}
}

func parseConnectEnd(data []byte) error {
	var payload struct {
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	if payload.Error != nil {
		return errString("Connect error " + payload.Error.Code + ": " + payload.Error.Message)
	}
	return nil
}

type errString string

func (e errString) Error() string { return string(e) }
