package claweb

type clawebHelloFrame struct {
	Type     string `json:"type"`
	Token    string `json:"token"`
	ClientID string `json:"clientId,omitempty"`
	UserID   string `json:"userId,omitempty"`
	RoomID   string `json:"roomId,omitempty"`
}

type clawebMessageFrame struct {
	Type          string `json:"type"`
	ID            string `json:"id,omitempty"`
	MessageID     string `json:"messageId,omitempty"`
	Role          string `json:"role,omitempty"`
	Text          string `json:"text,omitempty"`
	MediaURL      string `json:"mediaUrl,omitempty"`
	MediaType     string `json:"mediaType,omitempty"`
	MediaDataURL  string `json:"mediaDataUrl,omitempty"`
	MediaFilename string `json:"mediaFilename,omitempty"`
	ReplyTo       string `json:"replyTo,omitempty"`
	ReplyPreview  string `json:"replyPreview,omitempty"`
	Timestamp     int64  `json:"timestamp,omitempty"`
}

type clawebReadyFrame struct {
	Type          string `json:"type"`
	ServerVersion string `json:"serverVersion,omitempty"`
}

type clawebErrorFrame struct {
	Type    string `json:"type"`
	ID      string `json:"id,omitempty"`
	Message string `json:"message"`
}

func (f clawebMessageFrame) resolvedID() string {
	if f.ID != "" {
		return f.ID
	}
	return f.MessageID
}
