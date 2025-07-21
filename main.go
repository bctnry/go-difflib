package difflib

// this code sucks. i shall fix this in the future...

const (
	APPEND uint8 = 1
	DELETE uint8 = 2
	CHANGE uint8 = 3
	SAME uint8 = 4
)

// this is here because UnreadByte somehow can't unread a whole line.
type trueLineReader struct {
	reader *bufio.Reader
	buffer []string
}

func newTrueLineReader(r *bufio.Reader) *trueLineReader {
	return &trueLineReader{
		reader: r,
		buffer: make([]string, 0),
	}
}
func (r *trueLineReader) readLine() (string, error) {
	if len(r.buffer) > 0 {
		rb, e := r.buffer[:len(r.buffer)-1], r.buffer[len(r.buffer)-1]
		r.buffer = rb
		return e, nil
	}
	return r.reader.ReadString('\n')
}
func (r *trueLineReader) unreadLine(l string) {
	r.buffer = append(r.buffer, l)
}


type POSIXDiffSection struct {
	Type uint8
	ArgL1, ArgL2, ArgR1, ArgR2 int64
	File1Lines []string
	File2Lines []string
}

type POSIXDiff struct {
	SectionList []*POSIXDiffSection
}

var ErrInvalidFormat = errors.New("Invalid format")

func ParsePOSIXDiff(s io.Reader) (*POSIXDiff, error) {
	re, err := regexp.Compile("([0-9]+)(?:,([0-9]+))?([acd])([0-9]+)(?:,([0-9]+))?")
	if err != nil { return nil, err }
	br := bufio.NewReader(s)
	tlr := newTrueLineReader(br)
	diffseclist := make([]*POSIXDiffSection, 0)
	for {
		l, err := tlr.readLine()
		if errors.Is(err, io.EOF) { break }
		if err != nil { return nil, err }
		if len(l) <= 0 { break }
		r := re.FindStringSubmatch(l)
		if len(r) <= 0 { break }
		if len(r[3]) <= 0 { break }
		var (
			argL1Num int64 = 0
			argL2Num int64 = 0
			argR1Num int64 = 0
			argR2Num int64 = 0
		)
		argL1Num, err = strconv.ParseInt(r[1], 10, 64)
		if err != nil { return nil, err }
		if len(r[2]) > 0 {
			argL2Num, err = strconv.ParseInt(r[2], 10, 64)
			if err != nil { return nil, err }
		}
		argR1Num, err = strconv.ParseInt(r[4], 10, 64)
		if err != nil { return nil, err }
		if len(r[5]) > 0 {
			argR2Num, err = strconv.ParseInt(r[5], 10, 64)
			if err != nil { return nil, err }
		}
		var t uint8 = 0
		switch r[3][0] {
		case 'a': t = APPEND
		case 'd': t = DELETE
		case 'c': t = CHANGE
		}
		var leftdiffsize int64
		var rightdiffsize int64
		switch t {
		case APPEND:
			leftdiffsize = 0
			rightdiffsize = argR2Num - argR1Num + 1
		case DELETE:
			leftdiffsize = argL2Num - argL1Num + 1
			rightdiffsize = 0
		case CHANGE:
			if len(r[2]) <= 0 {
				leftdiffsize = 1
			} else {
				leftdiffsize = argL2Num - argL1Num + 1
			}
			if len(r[5]) <= 0 {
				rightdiffsize = 1
			} else {
				rightdiffsize = argR2Num - argR1Num + 1
			}
		}
		var left []string = nil
		if leftdiffsize > 0 { left = make([]string, 0) }
		var right []string = nil
		if rightdiffsize > 0 { right = make([]string, 0) }
		for range leftdiffsize {
			l, err := tlr.readLine()
			if err != nil { return nil, err }
			left = append(left, l[2:])
		}
		if leftdiffsize > 0 && rightdiffsize > 0 {
			l, err := br.ReadString('\n')
			if err != nil { return nil, err }
			if strings.Compare(l, "---\n") != 0 {
				return nil, ErrInvalidFormat
			}
		}
		for range rightdiffsize {
			l, err := tlr.readLine()
			if err != nil { return nil, err }
			right = append(right, l[2:])
		}
		diffseclist = append(diffseclist, &POSIXDiffSection{
			Type: t,
			ArgL1: argL1Num,
			ArgL2: argL2Num,
			ArgR1: argR1Num,
			ArgR2: argR2Num,
			File1Lines: left,
			File2Lines: right,
		})
	}
	return &POSIXDiff{
		SectionList: diffseclist,
	}, nil
}

type AnnotatedLine struct {
	Type uint8
	Line string
}

type ContextDiffPatch struct {
	Start, End int64
	Lines []AnnotatedLine
}

type ContextDiff struct {
	File1Name, File2Name string
	File1Timestamp, File2Timestamp int64
	File1Patch []ContextDiffPatch
	File2Patch []ContextDiffPatch
}

var ErrNotValidTimestampFormat = errors.New("Not valid timestamp")
func parseTimezoneOffset(s string) (int, error) {
	if s == "Z" { return 0, nil }
	if len(s) != 5 { return 0, errors.New("Invalid timezone offset string") }

	hour := (int(s[1]) - int('0') * 10) + (int(s[2]) - int('0'))
	minute := (int(s[3]) - int('0') * 10) + (int(s[4]) - int('0'))
	total := (hour * 60 + minute) * 60
	if s[0] == '-' { total = -total }
	return total, nil
}
func parseTimestamp(s string) (int64, error) {
	// parse timestamp.
	// despite what The Open Group's document says about the format
	// of diff-c, the output of GNU diffutils seems to be the same
	// as diff-u. this is better because having locale-dependent
	// thing sucks big ass for obvious reasons.
	r, err := regexp.Compile("(\\d+)-(\\d+)-(\\d+)\\s+(\\d+):(\\d+):(\\d+)(?:\\.(\\d+))?\\s+([+-]\\d+)")
	if err != nil { return 0, err }
	m := r.FindStringSubmatch(strings.TrimSpace(s))
	if len(m) <= 0 { return 0, ErrNotValidTimestampFormat }
	year, _ := strconv.ParseInt(m[1], 10, 64)
	month, _ := strconv.ParseInt(m[2], 10, 64)
	day, _ := strconv.ParseInt(m[3], 10, 64)
	hour, _ := strconv.ParseInt(m[4], 10, 64)
	minute, _ := strconv.ParseInt(m[5], 10, 64)
	second, _ := strconv.ParseInt(m[6], 10, 64)
	frac, _ := strconv.ParseInt(m[7], 10, 64)
	timezoneInt, _ := parseTimezoneOffset(m[8])
	timezone := time.FixedZone("UTC" + m[8], timezoneInt)
	d := time.Date(
		int(year),
		time.Month(int(month)),
		int(day),
		int(hour),
		int(minute),
		int(second),
		int(frac),
		timezone,
	)
	return d.Unix(), nil
}

func ParseContextDiff(r io.Reader) (*ContextDiff, error) {
	header1, err := regexp.Compile("\\*\\*\\* (.*)\t(.*)")
	if err != nil { return nil, err }
	header2, err := regexp.Compile("--- (.*)\t(.*)")
	if err != nil { return nil, err }
	br := bufio.NewReader(r)
	tlr := newTrueLineReader(br)
	header1line, err := tlr.readLine()
	if err != nil { return nil, err }
	header2line, err := tlr.readLine()
	if err != nil { return nil, err }
	matchres := header1.FindStringSubmatch(header1line)
	if len(matchres) <= 0 { return nil, ErrInvalidFormat }
	file1Path := strings.TrimSpace(matchres[1])
	file1Timestamp, _ := parseTimestamp(matchres[2])
	matchres = header2.FindStringSubmatch(header2line)
	file2Path := strings.TrimSpace(matchres[1])
	file2Timestamp, _ := parseTimestamp(matchres[2])
	file1head, err := regexp.Compile(`\*\*\* (\d+),(\d+) \*\*\*\*`)
	if err != nil { return nil, err }
	file2head, err := regexp.Compile(`--- (\d+),(\d+) ----`)
	if err != nil { return nil, err }
	f1section := make([]ContextDiffPatch, 0)
	f2section := make([]ContextDiffPatch, 0)
	for {
		l, err := tlr.readLine()
		if errors.Is(err, io.EOF) { break }
		if err != nil { return nil, err }
		if len(l) <= 0 { break; }
		if strings.TrimSpace(l) != "***************" { return nil, ErrInvalidFormat }
		l, err = tlr.readLine()
		if errors.Is(err, io.EOF) { break }
		if err != nil { return nil, err }
		matchres = file1head.FindStringSubmatch(l)
		if len(matchres) <= 0 { return nil, ErrInvalidFormat }
		startStr := matchres[1]
		endStr := matchres[2]
		start, _ := strconv.ParseInt(startStr, 10, 64)
		end, _ := strconv.ParseInt(endStr, 10, 64)
		file1p := make([]AnnotatedLine, 0)
		for {
			l, err = tlr.readLine()
			if errors.Is(err, io.EOF) { break }
			if err != nil { return nil, err }
			pfx := l[:2]
			if pfx != "  " && pfx != "+ " && pfx != "- " && pfx != "! " {
				tlr.unreadLine(l)
				break
			}
			lineContent := l[2:]
			var lineType uint8
			switch pfx {
			case "  ": lineType = SAME
			case "+ ": lineType = APPEND
			case "- ": lineType = DELETE
			case "! ": lineType = CHANGE
			}
			file1p = append(file1p, AnnotatedLine{
				Type: lineType,
				Line: lineContent,
			})
		}
		f1section = append(f1section, ContextDiffPatch{
			Start: start,
			End: end,
			Lines: file1p,
		})
		l, err = tlr.readLine()
		if errors.Is(err, io.EOF) { break }
		if err != nil { return nil, err }
		matchres = file2head.FindStringSubmatch(l)
		if len(matchres) <= 0 { continue }
		startStr = matchres[1]
		endStr = matchres[2]
		start, _ = strconv.ParseInt(startStr, 10, 64)
		end, _ = strconv.ParseInt(endStr, 10, 64)
		file2p := make([]AnnotatedLine, 0)
		for {
			l, err = tlr.readLine()
			if errors.Is(err, io.EOF) { break }
			if err != nil { return nil, err }
			pfx := l[:2]
			if pfx != "  " && pfx != "+ " && pfx != "- " && pfx != "! " {
				tlr.unreadLine(l)
				break
			}
			lineContent := l[2:]
			var lineType uint8
			switch pfx {
			case "  ": lineType = SAME
			case "+ ": lineType = APPEND
			case "- ": lineType = DELETE
			case "! ": lineType = CHANGE
			}
			file1p = append(file1p, AnnotatedLine{
				Type: lineType,
				Line: lineContent,
			})
		}
		f2section = append(f2section, ContextDiffPatch{
			Start: start,
			End: end,
			Lines: file2p,
		})
	}
	return &ContextDiff{
		File1Name: file1Path,
		File2Name: file2Path,
		File1Timestamp: file1Timestamp,
		File2Timestamp: file2Timestamp,
		File1Patch: f1section,
		File2Patch: f2section,
	}, nil
}

type UnifiedDiffPatch struct {
	LStart, LLineCount, RStart, RLineCount int64
	Lines []AnnotatedLine
}

type UnifiedDiff struct {
	File1Name, File2Name string
	File1Timestamp, File2Timestamp int64
	PatchList []UnifiedDiffPatch
}


func ParseUnifiedDiff(r io.Reader) (*UnifiedDiff, error) {
	header1, err := regexp.Compile("--- (.*)\t(.*)")
	if err != nil { return nil, err }
	header2, err := regexp.Compile("\\+\\+\\+ (.*)\t(.*)")
	if err != nil { return nil, err }
	br := bufio.NewReader(r)
	tlr := newTrueLineReader(br)
	header1line, err := tlr.readLine()
	if err != nil { return nil, err }
	header2line, err := tlr.readLine()
	if err != nil { return nil, err }
	matchres := header1.FindStringSubmatch(header1line)
	if len(matchres) <= 0 { return nil, ErrInvalidFormat }
	file1Path := strings.TrimSpace(matchres[1])
	file1Timestamp, _ := parseTimestamp(matchres[2])
	matchres = header2.FindStringSubmatch(header2line)
	file2Path := strings.TrimSpace(matchres[1])
	file2Timestamp, _ := parseTimestamp(matchres[2])
	sectionHead, err := regexp.Compile(`@@ -(\d+),(\d+) \+(\d+),(\d+) @@`)
	if err != nil { return nil, err }
	sectionList := make([]UnifiedDiffPatch, 0)
	for {
		l, err := tlr.readLine()
		if errors.Is(err, io.EOF) { break }
		if err != nil { return nil, err }
		if len(l) <= 0 { break; }
		matchres = sectionHead.FindStringSubmatch(l)
		if len(matchres) <= 0 { return nil, ErrInvalidFormat }
		lStartStr := matchres[1]
		lLineCountStr := matchres[2]
		rStartStr := matchres[3]
		rLineCountStr := matchres[4]
		lStart, _ := strconv.ParseInt(lStartStr, 10, 64)
		lLineCount, _ := strconv.ParseInt(lLineCountStr, 10, 64)
		rStart, _ := strconv.ParseInt(rStartStr, 10, 64)
		rLineCount, _ := strconv.ParseInt(rLineCountStr, 10, 64)
		pLines := make([]AnnotatedLine, 0)
		for {
			l, err = tlr.readLine()
			if errors.Is(err, io.EOF) { break }
			if err != nil { return nil, err }
			if l[0] != ' ' && l[0] != '-' && l[0] != '+' {
				tlr.unreadLine(l)
				break
			}
			lineContent := l[1:]
			var lineType uint8
			switch l[0] {
			case ' ': lineType = SAME
			case '+': lineType = APPEND
			case '-': lineType = DELETE
			}
			pLines = append(pLines, AnnotatedLine{
				Type: lineType,
				Line: lineContent,
			})
		}
		sectionList = append(sectionList, UnifiedDiffPatch{
			LStart: lStart,
			LLineCount: lLineCount,
			RStart: rStart,
			RLineCount: rLineCount,
			Lines: pLines,
		})
	}
	return &UnifiedDiff{
		File1Name: file1Path,
		File2Name: file2Path,
		File1Timestamp: file1Timestamp,
		File2Timestamp: file2Timestamp,
		PatchList: sectionList,
	}, nil
	
}

