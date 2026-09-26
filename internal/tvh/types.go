package tvh

import "strings"

// ServerInfo is GET api/serverinfo.
type ServerInfo struct {
	SWVersion    string   `json:"sw_version"`
	APIVersion   FlexInt  `json:"api_version"`
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
}

// Subscription is one entry of api/status/subscriptions.
type Subscription struct {
	ID       FlexInt    `json:"id"`
	Start    FlexInt    `json:"start"`
	Errors   FlexInt    `json:"errors"`
	State    FlexString `json:"state"`
	Hostname FlexString `json:"hostname"`
	Username FlexString `json:"username"`
	Client   FlexString `json:"client"`
	Title    FlexString `json:"title"`
	Channel  FlexString `json:"channel"`
	Service  FlexString `json:"service"`
	Profile  FlexString `json:"profile"`
	In       FlexInt    `json:"in"`
	Out      FlexInt    `json:"out"`
	TotalIn  FlexInt    `json:"total_in"`
	TotalOut FlexInt    `json:"total_out"`
}

// Connection is one entry of api/status/connections.
type Connection struct {
	ID         FlexInt    `json:"id"`
	ServerPort FlexInt    `json:"server_port"`
	Peer       FlexString `json:"peer"`
	PeerPort   FlexInt    `json:"peer_port"`
	Started    FlexInt    `json:"started"`
	Streaming  FlexInt    `json:"streaming"`
	Type       FlexString `json:"type"`
	User       FlexString `json:"user"`
}

// Input is one entry of api/status/inputs.
type Input struct {
	UUID        string  `json:"uuid"`
	Input       string  `json:"input"`
	Stream      string  `json:"stream"`
	Subs        FlexInt `json:"subs"`
	Weight      FlexInt `json:"weight"`
	Signal      FlexInt `json:"signal"`
	SignalScale FlexInt `json:"signal_scale"`
	BER         FlexInt `json:"ber"`
	SNR         FlexInt `json:"snr"`
	SNRScale    FlexInt `json:"snr_scale"`
	UNC         FlexInt `json:"unc"`
	BPS         FlexInt `json:"bps"`
	TE          FlexInt `json:"te"`
	CC          FlexInt `json:"cc"`
	ECBit       FlexInt `json:"ec_bit"`
	TCBit       FlexInt `json:"tc_bit"`
	ECBlock     FlexInt `json:"ec_block"`
	TCBlock     FlexInt `json:"tc_block"`
}

// Network is one entry of api/mpegts/network/grid.
type Network struct {
	UUID        string   `json:"uuid"`
	Name        string   `json:"networkname"`
	Class       string   `json:"class"`
	Enabled     FlexBool `json:"enabled"`
	NumMux      FlexInt  `json:"num_mux"`
	NumSvc      FlexInt  `json:"num_svc"`
	NumChn      FlexInt  `json:"num_chn"`
	ScanQLength FlexInt  `json:"scanq_length"`
}

// Type derives the network type from the Tvheadend idnode class name (e.g.
// "iptv_network", "dvb_network_dvbs", "dvb_network_dvbt", "dvb_network_dvbc",
// "dvb_network_atsc_t"), per spec §4.3. Returns "unknown" when the grid does
// not expose a recognised class.
func (n Network) Type() string {
	switch {
	case strings.HasPrefix(n.Class, "iptv"):
		return "iptv"
	case strings.Contains(n.Class, "dvbs"):
		return "dvbs"
	case strings.Contains(n.Class, "dvbt"):
		return "dvbt"
	case strings.Contains(n.Class, "dvbc"):
		return "dvbc"
	case strings.Contains(n.Class, "atsc"):
		return "atsc"
	default:
		return "unknown"
	}
}

// Mux is one entry of api/mpegts/mux/grid.
type Mux struct {
	UUID       string   `json:"uuid"`
	Enabled    FlexBool `json:"enabled"`
	Network    string   `json:"network"`
	ScanResult FlexInt  `json:"scan_result"`
}

// ScanResultName maps scan_result codes to label values.
func (m Mux) ScanResultName() string {
	switch m.ScanResult {
	case 1:
		return "ok"
	case 2:
		return "fail"
	case 3:
		return "partial"
	default:
		return "none"
	}
}

// Service is one entry of api/mpegts/service/grid.
type Service struct {
	UUID      string   `json:"uuid"`
	Network   string   `json:"network"`
	Enabled   FlexBool `json:"enabled"`
	Encrypted FlexBool `json:"encrypted"`
	Channel   []string `json:"channel"`
}

// Channel is one entry of api/channel/grid.
type Channel struct {
	UUID    string   `json:"uuid"`
	Enabled FlexBool `json:"enabled"`
	Name    string   `json:"name"`
	Tags    []string `json:"tags"`
}

// ChannelTag is one entry of api/channeltag/grid.
type ChannelTag struct {
	UUID    string   `json:"uuid"`
	Enabled FlexBool `json:"enabled"`
	Name    string   `json:"name"`
}

// AccessEntry is one entry of api/access/entry/grid (no secrets here).
type AccessEntry struct {
	UUID      string   `json:"uuid"`
	Enabled   FlexBool `json:"enabled"`
	Username  string   `json:"username"`
	ConnLimit FlexInt  `json:"conn_limit"`
	Admin     FlexBool `json:"admin"`
}

// DVREntry is one entry of api/dvr/entry/grid. Titles are intentionally not decoded.
type DVREntry struct {
	UUID        string  `json:"uuid"`
	SchedStatus string  `json:"sched_status"`
	Errors      FlexInt `json:"errors"`
	DataErrors  FlexInt `json:"data_errors"`
	Filesize    FlexInt `json:"filesize"`
	Owner       string  `json:"owner"`
	Creator     string  `json:"creator"`
	ConfigUUID  string  `json:"config_name"`
}

// DVRConfig is one entry of api/dvr/config/grid.
type DVRConfig struct {
	UUID    string `json:"uuid"`
	Name    string `json:"name"`
	Profile string `json:"profile"`
	Storage string `json:"storage"`
}

// DisplayName returns name or uuid when the name is blank.
func (d DVRConfig) DisplayName() string {
	if d.Name == "" {
		return d.UUID
	}
	return d.Name
}
