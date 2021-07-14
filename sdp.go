package sdp

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

var (
	ErrSyntax  = errors.New("syntax error")
	ErrInvalid = errors.New("invalid")
)

const (
	NetTypeIN = "IN"
	AddrType4 = "IP4"
	AddrType6 = "IP6"
)

const (
	MediaAudio = "audio"
	MediaVideo = "video"
	MediaText  = "text"
	MediaApp   = "application"
	MediaMesg  = "message"
)

const epoch = 2208988800

type Bandwidth struct {
	Type  string
	Value int64
}

type Attribute struct {
	Name  string
	Value string
}

type ConnInfo struct {
	NetType  string
	AddrType string
	Addr     string
	TTL      int64
}

type Session struct {
	User string
	ID   int64
	Ver  int64
	ConnInfo

	Name string
	Info string
	URI  string
}

type Interval struct {
	Starts time.Time
	Ends   time.Time
}

func (i Interval) IsUnbound() bool {
	return i.Ends.IsZero()
}

func (i Interval) IsPermanent() bool {
	return i.Starts.IsZero() && i.Ends.IsZero()
}

type MediaInfo struct {
	Media string
	Port  uint16
	Count uint16
	Proto string
	Attrs []string

	Info string

	ConnInfo   ConnInfo
	Bandwidth  []Bandwidth
	Attributes []Attribute
}

func (m MediaInfo) PortRange() []uint16 {
	if m.Count == 0 {
		return []uint16{m.Port}
	}
	var arr []uint16
	for i := 0; i < m.Count; i++ {
		arr = append(arr, m.Port+i)
	}
	return arr
}

type File struct {
	Version int
	Session

	Email []string
	Phone []string

	ConnInfo
	Bandwidth  []Bandwidth
	Attributes []Attribute

	Intervals []Interval

	Medias []MediaInfo
}

func (f File) Types() []string {
	var arr []string
	for i := range f.Medias {
		arr = append(arr, f.Medias[i])
	}
	return arr
}

func Parse(r io.Reader) (File, error) {
	var (
		rs   = bufio.NewReader(r)
		file File
	)
	for i := range parsers {
		p := parsers[i]
		if err := p.parse(&file, rs, p.prefix); err != nil {
			return file, err
		}
	}
	return file, nil
}

var parsers = []struct {
	prefix string
	parse  func(*File, *bufio.Reader, string) error
}{
	{prefix: "v", parse: parseVersion},
	{prefix: "o", parse: parseOrigin},
	{prefix: "s", parse: parseName},
	{prefix: "i", parse: parseInfo},
	{prefix: "u", parse: parseURI},
	{prefix: "e", parse: parseEmail},
	{prefix: "p", parse: parsePhone},
	{prefix: "c", parse: parseConnInfo},
	{prefix: "b", parse: parseBandwidth},
	{prefix: "a", parse: parseAttributes},
	{prefix: "t", parse: parseInterval},
	{prefix: "r", parse: skip},
	{prefix: "z", parse: skip},
	{prefix: "m", parse: parseMedia},
}

var mediaparsers = []struct {
	prefix string
	parse  func(*MediaInfo, *bufio.Reader, string) error
}{
	{prefix: "i", parse: parseMediaInfo},
	{prefix: "c", parse: parseMediaConnInfo},
	{prefix: "b", parse: parseMediaBandwidth},
	{prefix: "a", parse: parseMediaAttributes},
}

func parseMedia(file *File, rs *bufio.Reader, prefix string) error {
	for {
		if !hasPrefix(rs, prefix) {
			break
		}
		line, err := checkLine(rs, prefix)
		if err != nil {
			return err
		}
		mi, err := parseMediaDescription(line, rs)
		if err != nil {
			return err
		}
		file.Medias = append(file.Medias, mi)
	}
	return nil
}

func parseMediaDescription(line string, rs *bufio.Reader) (MediaInfo, error) {
	var (
		mi    MediaInfo
		err   error
		parts = split(line)
	)
	if len(parts) < 4 {
		return mi, ErrSyntax
	}
	mi.Media = parts[0]
	if x := strings.Index(parts[1], "/"); x > 0 {
		var n uint64
		if n, err = strconv.ParseUint(parts[1][x:], 10, 16); err != nil {
			return mi, err
		}
		mi.Port = uint16(n)
		if n, err = strconv.ParseUint(parts[1][x+1:], 10, 16); err != nil {
			return mi, err
		}
		mi.Count = uint16(n)
	} else {
		n, err := strconv.ParseUint(parts[1], 10, 16)
		if err != nil {
			return mi, err
		}
		mi.Port = uint16(n)
	}
	mi.Proto = parts[2]
	mi.Attrs = append(mi.Attrs, parts[2:]...)
	for i := range mediaparsers {
		p := mediaparsers[i]
		if err := p.parse(&mi, rs, p.prefix); err != nil {
			return mi, err
		}
	}
	return mi, nil
}

func parseInterval(file *File, rs *bufio.Reader, prefix string) error {
	parse := func(str string) (time.Time, error) {
		n, err := strconv.ParseInt(str, 10, 64)
		if err != nil || n == 0 {
			return time.Time{}, err
		}
		return time.Unix(n-epoch, 0).UTC(), nil
	}
	for {
		if !hasPrefix(rs, prefix) {
			break
		}
		line, err := checkLine(rs, prefix)
		if err != nil {
			return err
		}
		parts := split(line)
		if len(parts) != 2 {
			return ErrSyntax
		}
		var i Interval
		if i.Starts, err = parse(parts[0]); err != nil {
			return err
		}
		if i.Ends, err = parse(parts[1]); err != nil {
			return err
		}
		file.Intervals = append(file.Intervals, i)
	}
	return nil
}

func parseAttributes(file *File, rs *bufio.Reader, prefix string) error {
	var err error
	file.Attributes, err = parseAttributeLines(rs, prefix)
	return err
}

func parseMediaAttributes(media *MediaInfo, rs *bufio.Reader, prefix string) error {
	var err error
	media.Attributes, err = parseAttributeLines(rs, prefix)
	return err
}

func parseBandwidth(file *File, rs *bufio.Reader, prefix string) error {
	var err error
	file.Bandwidth, err = parseBandwidthLines(rs, prefix)
	return err
}

func parseMediaBandwidth(media *MediaInfo, rs *bufio.Reader, prefix string) error {
	var err error
	media.Bandwidth, err = parseBandwidthLines(rs, prefix)
	return err
}

func parseConnInfo(file *File, rs *bufio.Reader, prefix string) error {
	line, err := setString(rs, prefix, false)
	if err != nil || line == "" {
		return err
	}
	file.ConnInfo, err = parseConnectionInfo(split(line))
	return err
}

func parseMediaConnInfo(media *MediaInfo, rs *bufio.Reader, prefix string) error {
	line, err := setString(rs, prefix, false)
	if err != nil || line == "" {
		return err
	}
	media.ConnInfo, err = parseConnectionInfo(split(line))
	return err
}

func parsePhone(file *File, rs *bufio.Reader, prefix string) error {
	var err error
	file.Phone, err = setArray(rs, prefix)
	return err
}

func parseEmail(file *File, rs *bufio.Reader, prefix string) error {
	var err error
	file.Email, err = setArray(rs, prefix)
	return err
}

func parseURI(file *File, rs *bufio.Reader, prefix string) error {
	var err error
	file.Session.URI, err = setString(rs, prefix, false)
	return err
}

func parseInfo(file *File, rs *bufio.Reader, prefix string) error {
	var err error
	file.Session.Info, err = setString(rs, prefix, false)
	return err
}

func parseMediaInfo(media *MediaInfo, rs *bufio.Reader, prefix string) error {
	var err error
	media.Info, err = setString(rs, prefix, false)
	return err
}

func parseName(file *File, rs *bufio.Reader, prefix string) error {
	var err error
	file.Session.Name, err = setString(rs, prefix, true)
	if err == nil && file.Session.Name == "" {
		err = fmt.Errorf("empty session name")
	}
	return err
}

// o=<username> <sess-id> <sess-version> <nettype> <addrtype> <unicast-address>
func parseOrigin(file *File, rs *bufio.Reader, prefix string) error {
	line, err := checkLine(rs, prefix)
	if err != nil {
		return err
	}
	parts := split(line)
	if len(parts) != 6 {
		return ErrSyntax
	}
	if parts[0] != "-" {
		file.Session.User = parts[0]
	}
	if file.Session.ID, err = strconv.ParseInt(parts[1], 10, 64); err != nil {
		return fmt.Errorf("%w - session id: %s", ErrSyntax, err)
	}
	if file.Session.Ver, err = strconv.ParseInt(parts[2], 10, 64); err != nil {
		return fmt.Errorf("%w - session version: %s", ErrSyntax, err)
	}
	file.Session.ConnInfo, err = parseConnectionInfo(parts[3:])
	return err
}

func parseConnectionInfo(parts []string) (ConnInfo, error) {
	var ci ConnInfo
	if len(parts) != 3 {
		return ci, fmt.Errorf("%w: not enough elemnt in line %s", ErrSyntax, parts)
	}
	if parts[0] != NetTypeIN {
		return ci, fmt.Errorf("%w - net type: unknown value %s", parts[0])
	}
	if parts[1] != AddrType4 && parts[1] != AddrType6 {
		return ci, fmt.Errorf("%w - addr type: unknown value %s", parts[1])
	}
	ci.NetType = parts[0]
	ci.AddrType = parts[1]
	ci.Addr = parts[2]
	if x := strings.Index(ci.Addr, "/"); x > 0 {
		var err error
		if ci.TTL, err = strconv.ParseInt(ci.Addr[x+1:], 10, 16); err != nil {
			return ci, err
		}
	}
	return ci, nil
}

func parseVersion(file *File, rs *bufio.Reader, prefix string) error {
	line, err := checkLine(rs, prefix)
	if err != nil {
		return err
	}
	file.Version, err = strconv.Atoi(line)
	if file.Version != 0 {
		return fmt.Errorf("%w: unsupported version", ErrInvalid)
	}
	return err
}

func skip(_ *File, rs *bufio.Reader, prefix string) error {
	for {
		if !hasPrefix(rs, prefix) {
			break
		}
		_, err := checkLine(rs, prefix)
		if err != nil {
			return err
		}
	}
	return nil
}

func parseAttributeLines(rs *bufio.Reader, prefix string) ([]Attribute, error) {
	var (
		arr []Attribute
		atb Attribute
	)
	for hasPrefix(rs, prefix) {
		line, err := checkLine(rs, prefix)
		if err != nil {
			return nil, err
		}
		x := strings.Index(line, ":")
		if x < 0 {
			x = len(line)
		}
		atb.Name = line[:x]
		atb.Value = line[x:]
		arr = append(arr, atb)
	}
	return arr, nil
}

func parseBandwidthLines(rs *bufio.Reader, prefix string) ([]Bandwidth, error) {
	var (
		arr []Bandwidth
		bwd Bandwidth
	)
	for hasPrefix(rs, prefix) {
		line, err := checkLine(rs, prefix)
		if err != nil {
			return nil, err
		}
		x := strings.Index(line, ":")
		if x <= 0 || x >= len(line)-1 {
			return nil, fmt.Errorf("%w: parsing bandwidth (%s)", ErrSyntax, line)
		}
		bwd.Type = line[:x]
		if bwd.Value, err = strconv.ParseInt(line[x+1:], 10, 64); err != nil {
			return nil, err
		}
		arr = append(arr, bwd)
	}
	return arr, nil
}

func split(line string) []string {
	return strings.Split(line, " ")
}

func setString(rs *bufio.Reader, prefix string, required bool) (string, error) {
	if !required && !hasPrefix(rs, prefix) {
		return "", nil
	}
	return checkLine(rs, prefix)
}

func setArray(rs *bufio.Reader, prefix string) ([]string, error) {
	var arr []string
	for {
		if !hasPrefix(rs, prefix) {
			break
		}
		line, err := checkLine(rs, prefix)
		if err != nil {
			return nil, err
		}
		arr = append(arr, line)
	}
	return arr, nil
}

func hasPrefix(rs *bufio.Reader, prefix string) bool {
	peek, _ := rs.Peek(len(prefix))
	return string(peek) == prefix
}

func checkLine(rs *bufio.Reader, prefix string) (string, error) {
	line, err := rs.ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.TrimRight(line, "\r\n")
	prefix += "="
	if !strings.HasPrefix(line, prefix) {
		return "", fmt.Errorf("%w: missing prefix %s", ErrSyntax, prefix)
	}
	return line[len(prefix):], nil
}
