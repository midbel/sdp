package sdp

import (
	"bufio"
	"bytes"
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

	ModeIncl = "incl"
	ModeExcl = "excl"
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

func findAttributes(name string, attrs []Attribute) (Attribute, bool) {
	for i := range attrs {
		if attrs[i].Name == name {
			return attrs[i], true
		}
	}
	return Attribute{}, false
}

type ConnInfo struct {
	NetType  string
	AddrType string
	Addr     string
	TTL      int64
}

func (c ConnInfo) IsZero() bool {
	return c.NetType == "" && c.AddrType == "" && c.Addr == ""
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

type SourceInfo struct {
	Mode     string
	NetType  string
	AddrType string
	Addr     string
	List     []string
}

func (s SourceInfo) Include() bool {
	return s.Mode == ModeIncl
}

func parseSourceInfo(line string) (SourceInfo, error) {
	var (
		parts = split(line)
		size  = len(line)
		info  SourceInfo
	)
	if size < 5 {
		return info, ErrSyntax
	}
	if err := validModeType(parts[0]); err != nil {
		return info, err
	}
	if err := validNetType(parts[1]); err != nil {
		return info, err
	}
	if err := validAddrType(parts[2], true); err != nil {
		return info, err
	}
	info.Mode = parts[0]
	info.NetType = parts[1]
	info.AddrType = parts[2]
	info.Addr = parts[3]
	info.List = append(info.List, parts[4:]...)
	return info, nil
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
	for i := 0; i < int(m.Count); i++ {
		arr = append(arr, m.Port+uint16(i))
	}
	return arr
}

func (m MediaInfo) SourceFilter() (SourceInfo, error) {
	a, ok := findAttributes("source-filter", m.Attributes)
	if !ok {
		return SourceInfo{}, fmt.Errorf("source-filter not set")
	}
	return parseSourceInfo(a.Value)
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

func (f File) Dump() string {
	var buf bytes.Buffer
	writePrefix(&buf, 'v')
	buf.WriteString(strconv.Itoa(f.Version))
	writeEOL(&buf)
	writeSession(&buf, f.Session)
	for i := range f.Email {
		writePrefix(&buf, 'e')
		writeLine(&buf, f.Email[i])
	}
	for i := range f.Phone {
		writePrefix(&buf, 'p')
		writeLine(&buf, f.Phone[i])
	}
	writeConnInfo(&buf, f.ConnInfo, true)
	writeBandwidths(&buf, f.Bandwidth)
	writeAttributes(&buf, f.Attributes)
	writeIntervals(&buf, f.Intervals)
	for i := range f.Medias {
		writeMediaInfo(&buf, f.Medias[i])
	}
	return buf.String()
}

func (f File) Types() []string {
	var arr []string
	for i := range f.Medias {
		arr = append(arr, f.Medias[i].Media)
	}
	return arr
}

func (f File) SourceFilter() (SourceInfo, error) {
	a, ok := findAttributes("source-filter", f.Attributes)
	if !ok {
		return SourceInfo{}, fmt.Errorf("source-filter not set")
	}
	return parseSourceInfo(a.Value)
}

func Parse(r io.Reader) (File, error) {
	var (
		rs   = bufio.NewReader(r)
		file File
	)
	for i := range parsers {
		p := parsers[i]
		if err := p.parse(&file, rs, p.prefix); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
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
	{prefix: "t", parse: parseInterval},
	{prefix: "a", parse: parseAttributes},
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
	mi.Attrs = append(mi.Attrs, parts[3:]...)
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
	if err := validNetType(parts[0]); err != nil {
		return ci, err
	}
	if err := validAddrType(parts[1], false); err != nil {
		return ci, err
	}
	ci.NetType = parts[0]
	ci.AddrType = parts[1]
	ci.Addr = parts[2]
	if x := strings.Index(ci.Addr, "/"); x > 0 {
		var err error
		if ci.TTL, err = strconv.ParseInt(ci.Addr[x+1:], 10, 16); err != nil {
			return ci, err
		}
		ci.Addr = ci.Addr[:x]
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
			atb.Name = line
			continue
		}
		atb.Name = line[:x]
		atb.Value = line[x+1:]
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
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	line = strings.TrimRight(line, "\r\n")
	prefix += "="
	if !strings.HasPrefix(line, prefix) {
		return "", fmt.Errorf("%w: missing prefix %s", ErrSyntax, prefix)
	}
	return line[len(prefix):], nil
}

func validAddrType(str string, star bool) error {
	switch str {
	case AddrType4, AddrType6:
	default:
		if !star && str != "*" {
			return fmt.Errorf("%w: unknown addr type %s", ErrInvalid, str)
		}
	}
	return nil
}

func validNetType(str string) error {
	if str == NetTypeIN {
		return nil
	}
	return fmt.Errorf("%w: unknown net type %s", ErrInvalid, str)
}

func validModeType(str string) error {
	if str == ModeIncl || str == ModeExcl {
		return nil
	}
	return fmt.Errorf("%w: unknown mode type %s", ErrInvalid, str)
}

func writeIntervals(buf *bytes.Buffer, is []Interval) {
	convert := func(t time.Time) string {
		if t.IsZero() {
			return "0"
		}
		return strconv.FormatInt(t.Unix()+epoch, 10)
	}
	for i := range is {
		writePrefix(buf, 't')
		buf.WriteString(convert(is[i].Starts))
		buf.WriteByte(' ')
		buf.WriteString(convert(is[i].Ends))
		writeEOL(buf)
	}
}

func writeSession(buf *bytes.Buffer, sess Session) {
	writePrefix(buf, 'o')
	if sess.User == "" {
		sess.User = "-"
	}
	buf.WriteString(sess.User)
	buf.WriteByte(' ')
	buf.WriteString(strconv.FormatInt(sess.ID, 10))
	buf.WriteByte(' ')
	buf.WriteString(strconv.FormatInt(sess.Ver, 10))
	buf.WriteByte(' ')
	writeConnInfo(buf, sess.ConnInfo, false)

	writePrefix(buf, 's')
	writeLine(buf, sess.Name)
	if sess.Info != "" {
		writePrefix(buf, 'i')
		writeLine(buf, sess.Info)
	}
	if sess.URI != "" {
		writePrefix(buf, 'u')
		writeLine(buf, sess.URI)
	}
}

func writeMediaInfo(buf *bytes.Buffer, m MediaInfo) {
	writePrefix(buf, 'm')
	buf.WriteString(m.Media)
	buf.WriteByte(' ')
	buf.WriteString(strconv.FormatUint(uint64(m.Port), 10))
	if m.Count > 0 {
		buf.WriteByte('/')
		buf.WriteString(strconv.FormatUint(uint64(m.Count), 10))
	}
	buf.WriteByte(' ')
	buf.WriteString(m.Proto)
	for i := range m.Attrs {
		buf.WriteByte(' ')
		buf.WriteString(m.Attrs[i])
	}
	writeEOL(buf)
	if m.Info != "" {
		writePrefix(buf, 'i')
		writeLine(buf, m.Info)
	}
	writeConnInfo(buf, m.ConnInfo, true)
	writeBandwidths(buf, m.Bandwidth)
	writeAttributes(buf, m.Attributes)
}

func writeConnInfo(buf *bytes.Buffer, conn ConnInfo, prefix bool) {
	if conn.IsZero() {
		return
	}
	if prefix {
		writePrefix(buf, 'c')
	}
	buf.WriteString(conn.NetType)
	buf.WriteByte(' ')
	buf.WriteString(conn.AddrType)
	buf.WriteByte(' ')
	buf.WriteString(conn.Addr)
	if conn.TTL > 0 {
		buf.WriteByte('/')
		buf.WriteString(strconv.FormatInt(conn.TTL, 10))
	}
	writeEOL(buf)
}

func writeBandwidths(buf *bytes.Buffer, bws []Bandwidth) {
	for i := range bws {
		writePrefix(buf, 'b')
		buf.WriteString(bws[i].Type)
		buf.WriteByte(':')
		buf.WriteString(strconv.FormatInt(bws[i].Value, 10))
		writeEOL(buf)
	}
}

func writeAttributes(buf *bytes.Buffer, attrs []Attribute) {
	for i := range attrs {
		writePrefix(buf, 'a')
		buf.WriteString(attrs[i].Name)
		buf.WriteByte(':')
		buf.WriteString(attrs[i].Value)
		writeEOL(buf)
	}
}

func writePrefix(buf *bytes.Buffer, prefix byte) {
	buf.WriteByte(prefix)
	buf.WriteByte('=')
}

func writeLine(buf *bytes.Buffer, line string) {
	io.WriteString(buf, line)
	writeEOL(buf)
}

func writeEOL(buf *bytes.Buffer) {
	buf.WriteByte('\r')
	buf.WriteByte('\n')
}
