package vm

var staticCode = `
//+pigeon: err.go

var (
	// errInvalidEncoding is returned when the source is not properly
	// utf8-encoded.
	errInvalidEncoding = errors.New("invalid encoding")

	// errNoMatch is returned if no match could be found and no other
	// error has been raised.
	errNoMatch = errors.New("no match found")
)

// errList cumulates the errors found by the parser. It is part
// of the supported API.
type errList []error

// ϡadd adds err to the list of errors.
func (e *errList) ϡadd(err error) {
	if err != nil {
		*e = append(*e, err)
	}
}

// ϡerr returns the error list as an error, or nil if the list is empty.
func (e errList) ϡerr() error {
	if len(e) == 0 {
		return nil
	}
	e.ϡdedupe()
	return e
}

// ϡdedupe removes duplicate error messages from the list.
func (e *errList) ϡdedupe() {
	var cleaned []error
	set := make(map[string]bool)
	for _, err := range *e {
		if msg := err.Error(); !set[msg] {
			set[msg] = true
			cleaned = append(cleaned, err)
		}
	}
	*e = cleaned
}

// Error returns the error message for the errList. It implements the
// error interface.
func (e errList) Error() string {
	var buf bytes.Buffer

	for i, err := range e {
		if i > 0 {
			buf.WriteRune('\n')
		}
		buf.WriteString(err.Error())
	}
	return buf.String()
}

// parserError wraps an error with a prefix indicating the rule in which
// the error occurred. The original error is stored in the Inner field.
// It is part of the supported API.
type parserError struct {
	Inner   error
	ϡpos    position
	ϡprefix string
}

// Error returns the prefixed error message. It implements the error
// interface.
func (p *parserError) Error() string {
	return p.ϡprefix + ": " + p.Inner.Error()
}
//+pigeon: matchers.go

// ϡpeekReader is the interface that defines the peek and read
// methods.
type ϡpeekReader interface {
	peek() ϡsvpt
	read()
}

// ϡmatcher is the interface that defines the match method.
type ϡmatcher interface {
	match(ϡpeekReader) bool
}

// ϡanyMatcher is a matcher that matches any character but the
// EOF.
type ϡanyMatcher struct{}

// match tries to match a character in the peekReader.
func (a ϡanyMatcher) match(pr ϡpeekReader) bool {
	pt := pr.peek()
	pr.read()
	return pt.rn != utf8.RuneError
}

func (a ϡanyMatcher) String() string {
	return "."
}

// ϡstringMatcher is a matcher that matches a string.
type ϡstringMatcher struct {
	ignoreCase bool
	value      string // value must be lowercase if ignoreCase is true
}

// match tries to match the string in the peekReader.
func (s ϡstringMatcher) match(pr ϡpeekReader) bool {
	for _, want := range s.value {
		pt := pr.peek()
		if s.ignoreCase {
			pt.rn = unicode.ToLower(pt.rn)
		}
		if pt.rn != want {
			return false
		}
		pr.read()
	}
	return true
}

func (s ϡstringMatcher) String() string {
	v := strconv.Quote(s.value)
	if s.ignoreCase {
		v += "i"
	}
	return v
}

// ϡcharClassMatcher is a matcher that matches classes of characters.
type ϡcharClassMatcher struct {
	chars   []rune // runes must be lowercase if ignoreCase is true
	ranges  []rune // same for ranges
	classes []*unicode.RangeTable

	ignoreCase bool
	inverted   bool
}

func (c ϡcharClassMatcher) String() string {
	var buf bytes.Buffer

	buf.WriteString("[")
	if c.inverted {
		buf.WriteString("^")
	}
	for _, c := range c.chars {
		buf.WriteRune(c)
	}
	for i := 0; i < len(c.ranges); i += 2 {
		buf.WriteString(fmt.Sprintf("%c-%c", c.ranges[i], c.ranges[i+1]))
	}
	// unicode classes can't be stringified
	if l := len(c.classes); l > 0 {
		buf.WriteString(fmt.Sprintf("\\p{%d classes}", l))
	}
	buf.WriteString("]")
	if c.ignoreCase {
		buf.WriteString("i")
	}
	return buf.String()
}

// match tries to match classes of characters in the peekReader.
func (c ϡcharClassMatcher) match(pr ϡpeekReader) bool {
	pt := pr.peek()
	pr.read()

	if c.ignoreCase {
		pt.rn = unicode.ToLower(pt.rn)
	}

	// try to match in the list of available chars
	for _, rn := range c.chars {
		if pt.rn == rn {
			return !c.inverted
		}
	}

	// try to match in the list of ranges
	for i := 0; i < len(c.ranges); i += 2 {
		if pt.rn >= c.ranges[i] && pt.rn <= c.ranges[i+1] {
			return !c.inverted
		}
	}

	// try to match in the list of Unicode classes
	for _, cl := range c.classes {
		if unicode.Is(cl, pt.rn) {
			return !c.inverted
		}
	}

	return c.inverted
}

// ϡrangeTable returns the corresponding unicode range table from the
// provided class name.
func ϡrangeTable(class string) *unicode.RangeTable {
	if rt, ok := unicode.Categories[class]; ok {
		return rt
	}
	if rt, ok := unicode.Properties[class]; ok {
		return rt
	}
	if rt, ok := unicode.Scripts[class]; ok {
		return rt
	}

	// cannot happen
	panic(fmt.Sprintf("invalid Unicode class: %s", class))
}
//+pigeon: ops.go

// ϡop represents an opcode.
type ϡop byte

// list of opcodes in the pigeon VM.
const (
	ϡopExit ϡop = iota
	ϡopCall
	ϡopCallA
	ϡopCallB
	ϡopCumulOrF
	ϡopJump
	ϡopJumpIfF
	ϡopJumpIfT
	ϡopMatch
	ϡopNilIfF
	ϡopNilIfT
	ϡopPop
	ϡopPopVJumpIfF
	ϡopPush
	ϡopRestore
	ϡopRestoreIfF
	ϡopReturn
	ϡopStoreIfT
	ϡopTakeLOrJump
	ϡopmax // must always be after the last valid opcode

	// ϡopPlaceholder is an (invalid) opcode used by the Generator
	// to insert opcodes that need the index of the starting instruction
	// of a rule that hasn't been generated yet.
	//
	// It must be placed after ϡopmax (because it is invalid in the
	// final program) and it has one argument, the index in the strings
	// array of the identifier of the rule.
	ϡopPlaceholder
)

// ϡlookupOp translates an opcode to a string.
var ϡlookupOp = []string{
	ϡopExit: "exit", ϡopCall: "call", ϡopCallA: "callA",
	ϡopCallB: "callB", ϡopCumulOrF: "cumulOrF",
	ϡopJump: "jump", ϡopJumpIfF: "jumpIfF", ϡopJumpIfT: "jumpIfT",
	ϡopMatch: "match", ϡopNilIfF: "nilIfF", ϡopNilIfT: "nilIfT",
	ϡopPop: "pop", ϡopPopVJumpIfF: "popVJumpIfF",
	ϡopPush: "push", ϡopRestore: "restore", ϡopRestoreIfF: "restoreIfF",
	ϡopReturn: "return", ϡopStoreIfT: "storeIfT", ϡopTakeLOrJump: "takeLOrJump",
}

// String returns the string representation of the opcode.
func (op ϡop) String() string {
	if 0 <= op && int(op) < len(ϡlookupOp) {
		return ϡlookupOp[op]
	}
	return "ϡop(" + strconv.Itoa(int(op)) + ")"
}

// ϡinstr encodes an opcode with its arguments as a 64-bits unsigned
// integer. The bits are used as follows:
//
// o : 6 bits = opcode (max=63)
// n : 10 bits = for PUSHL, number of values in array (max=1023)
// l : 16 bits = instruction index (max=65535)
//
// So a single PUSH instruction can encode 2 indices (first arg is the stack ID).
// The 64-bit value looks like this:
// oooooonn nnnnnnnn llllllll llllllll llllllll llllllll llllllll llllllll
//
// And if a PUSH (L) instruction has more than 2 indices, it can store 4 full
// indices per subsequent values (4 * 16 bits = 64 bits).
type ϡinstr uint64

// limits and masks.
const (
	ϡiBits = 64
	ϡlBits = 16
	ϡnBits = 10
	ϡoBits = 6
	ϡlPerI = ϡiBits / ϡlBits

	ϡlMask = 1<<ϡlBits - 1
	ϡnMask = 1<<ϡnBits - 1
	ϡoMask = 1<<ϡoBits - 1
)

// decode decodes the instruction and returns the 5 parts:
// the opcode, the number of L array values, and the 3 instruction
// indices.
func (i ϡinstr) decode() (op ϡop, n, ix0, ix1, ix2 int) {
	ix2 = int(i & ϡlMask)
	i >>= ϡlBits
	ix1 = int(i & ϡlMask)
	i >>= ϡlBits
	ix0 = int(i & ϡlMask)
	i >>= ϡlBits
	n = int(i & ϡnMask)
	i >>= ϡnBits
	op = ϡop(i & ϡoMask)
	return
}

// decodeLs decodes the instruction as a list of L instruction
// indices (as a follow-up value to a PUSHL opcode).
func (i ϡinstr) decodeLs() (ix0, ix1, ix2, ix3 int) {
	ix3 = int(i & ϡlMask)
	i >>= ϡlBits
	ix2 = int(i & ϡlMask)
	i >>= ϡlBits
	ix1 = int(i & ϡlMask)
	i >>= ϡlBits
	ix0 = int(i & ϡlMask)
	return
}

// ϡencodeInstr encodes the provided operation and its arguments into
// a list of instruction values. It may return an error if any part
// of the instruction overflows the allowed values.
func ϡencodeInstr(op ϡop, args ...int) ([]ϡinstr, error) {
	var is []ϡinstr

	if op >= ϡopmax && op != ϡopPlaceholder {
		return nil, errors.New("invalid op value")
	}
	if len(args) > ϡnMask {
		return nil, errors.New("too many arguments")
	}

	// first instruction contains opcode
	is = append(is, ϡinstr(op)<<(ϡiBits-ϡoBits))
	n := uint(len(args))
	if n == 0 {
		return is, nil
	}
	off := uint(ϡiBits - ϡoBits - ϡnBits)
	is[0] |= ϡinstr(n) << off

	ix := 0
	for i, arg := range args {
		if arg > ϡlMask {
			return nil, errors.New("argument value too big")
		}

		mod := uint((i + 1) % ϡlPerI)
		if mod == 0 {
			is = append(is, 0)
			ix++
		}

		is[ix] |= ϡinstr(arg) << (off - (mod * ϡlBits))
	}

	return is, nil
}
//+pigeon: parser.go

// position records a position in the text. It is part of the supported
// API.
type position struct {
	// line is the 1-based index of the line of the current rune.
	line int
	// col is the 1-based index of the current rune on the line.
	col int
	// offset is the 0-based index of the starting byte of the current rune.
	offset int
}

// String formats a position as a string.
func (p position) String() string {
	return fmt.Sprintf("%d:%d (%d)", p.line, p.col, p.offset)
}

// current represents current matching data. It is the value on which
// action and predicate code blocks are generated as methods. It is
// part of the supported API.
type current struct {
	// pos holds the start position of the current match.
	pos position
	// text contains the raw text of the match. It is a slice in the
	// source data, so it should not be modified.
	text []byte
}

// ϡsvpt stores all state required to go back to a point in the
// parser.
type ϡsvpt struct {
	position
	rn rune
	w  int
}

// ϡparser parses the input text as rune code points.
type ϡparser struct {
	data []byte
	pt   ϡsvpt
	cur  current
}

// peek returns the current savepoint information.
func (p *ϡparser) peek() ϡsvpt {
	return p.pt
}

// read advances the parser to the next rune.
func (p *ϡparser) read() {
	p.pt.offset += p.pt.w
	rn, n := utf8.DecodeRune(p.data[p.pt.offset:])
	p.pt.rn = rn
	p.pt.w = n

	if rn == utf8.RuneError {
		if n > 0 {
			panic(errInvalidEncoding)
		}
	} else {
		p.pt.col++
		if rn == '\n' {
			p.pt.line++
			p.pt.col = 0
		}
	}
}

// sliceFrom gets the slice of bytes from the start savepoint to
// the current position, non inclusive.
func (p *ϡparser) sliceFrom(start ϡsvpt) []byte {
	return p.data[start.position.offset:p.pt.position.offset]
}
//+pigeon: pub.go

// Option is a function that can set an option on the parser. It returns
// the previous setting as an Option.
type Option func(*ϡvm) Option

// Debug creates an Option to set the debug flag to b. When set to true,
// debugging information is printed to stdout while parsing.
//
// The default is false.
func Debug(b bool) Option {
	return func(v *ϡvm) Option {
		old := v.debug
		v.debug = b
		return Debug(old)
	}
}

// Memoize creates an Option to set the memoize flag to b. When set to true,
// the parser will cache all results so each expression is evaluated only
// once. This guarantees linear parsing time even for pathological cases,
// at the expense of more memory and slower times for typical cases.
//
// The default is false.
func Memoize(b bool) Option {
	return func(v *ϡvm) Option {
		old := v.memoize
		v.memoize = b
		return Memoize(old)
	}
}

// Recover creates an Option to set the recover flag to b. When set to
// true, this causes the parser to recover from panics and convert it
// to an error. Setting it to false can be useful while debugging to
// access the full stack trace.
//
// The default is true.
func Recover(b bool) Option {
	return func(v *ϡvm) Option {
		old := v.recover
		v.recover = b
		return Recover(old)
	}
}

// ParseFile parses the file identified by filename.
func ParseFile(filename string, opts ...Option) (interface{}, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseReader(filename, f, opts...)
}

// ParseReader parses the data from r using filename as information in the
// error messages.
func ParseReader(filename string, r io.Reader, opts ...Option) (interface{}, error) {
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}

	return Parse(filename, b, opts...)
}

// Parse parses the data from b using filename as information in the
// error messages.
func Parse(filename string, b []byte, opts ...Option) (interface{}, error) {
	p := &ϡparser{
		data: b,
		pt:   ϡsvpt{position: position{line: 1}},
	}
	v := &ϡvm{
		filename: filename,
		parser:   p,
		recover:  true,
	}
	return v.setOptions(opts).run(ϡtheProgram)
}
//+pigeon: stacks.go

// ϡpstack implements the Position stack. It stores savepoints.
type ϡpstack []ϡsvpt

// push adds a value on the stack.
func (p *ϡpstack) push(pt ϡsvpt) {
	*p = append(*p, pt)
}

// pop removes a value from the stack.
func (p *ϡpstack) pop() ϡsvpt {
	n := len(*p)
	// if n == 0 {
	// 	panic("pstack is empty")
	// }
	v := (*p)[n-1]
	*p = (*p)[:n-1]
	return v
}

// ϡistack implements the Instruction index stack. It stores integers.
type ϡistack []int

// push adds a value on the stack.
func (i *ϡistack) push(v int) {
	*i = append(*i, v)
}

// pop removes a value from the stack.
func (i *ϡistack) pop() int {
	n := len(*i)
	// if n == 0 {
	// 	panic("istack is empty")
	// }
	v := (*i)[n-1]
	*i = (*i)[:n-1]
	return v
}

// ϡvstack implements the Value stack. It stores empty interfaces.
type ϡvstack []interface{}

// push adds a value on the stack.
func (v *ϡvstack) push(i interface{}) {
	*v = append(*v, i)
}

// pop removes a value from the stack.
func (v *ϡvstack) pop() interface{} {
	i := v.peek()
	*v = (*v)[:len(*v)-1]
	return i
}

// peek returns the value at the top of the stack, leaving it there.
func (v *ϡvstack) peek() interface{} {
	n := len(*v)
	// if n == 0 {
	// 	panic("vstack is empty")
	// }
	i := (*v)[n-1]
	return i
}

// ϡlstack implements the Loop stack. It stores slices of integers.
type ϡlstack [][]int

// push adds a value on the stack.
func (l *ϡlstack) push(a []int) {
	*l = append(*l, a)
}

// pop removes a value from the stack.
func (l *ϡlstack) pop() []int {
	n := len(*l)
	// if n == 0 {
	// 	panic("lstack is empty")
	// }
	a := (*l)[n-1]
	*l = (*l)[:n-1]
	return a
}

// take removes the integer at index 0 from the slice at the top of the
// stack. It returns -1 if the slice is empty. The slice is left on the
// stack.
func (l *ϡlstack) take() int {
	n := len(*l)
	// if n == 0 {
	// 	panic("lstack is empty")
	// }

	v := -1
	a := (*l)[n-1]
	if len(a) > 0 {
		v = a[0]
		(*l)[n-1] = a[1:]
	}
	return v
}

// ϡargsSet holds the list of arguments (key and value) to pass
// to the code blocks.
type ϡargsSet map[string]interface{}

// ϡastack is a stack of ϡargsSet.
type ϡastack []ϡargsSet

// push adds an empty ϡargsSet on top of the stack.
func (a *ϡastack) push() {
	*a = append(*a, ϡargsSet{})
}

// pop removes the top ϡargsSet from the stack.
func (a *ϡastack) pop() {
	n := len(*a)
	// if n == 0 {
	// 	panic("astack is empty")
	// }
	*a = (*a)[:n-1]
}

// peek returns the current top ϡargsSet.
func (a *ϡastack) peek() ϡargsSet {
	n := len(*a)
	// if n == 0 {
	// 	panic("astack is empty")
	// }
	as := (*a)[n-1]
	return as
}
//+pigeon: vm.go

// ϡsentinel is a type used to define sentinel values that shouldn't
// be equal to something else.
type ϡsentinel int

const (
	// ϡmatchFailed is a sentinel value used to indicate a match failure.
	ϡmatchFailed ϡsentinel = iota - 1
)

const (
	// stack IDs, used in PUSH and POP's first argument
	ϡpstackID = iota + 1
	ϡlstackID
	ϡvstackID
	ϡistackID
	ϡastackID

	// special V stack values
	ϡvValNil    = 0
	ϡvValFailed = 1
	ϡvValEmpty  = 2
)

var (
	ϡstackNm = []string{
		ϡpstackID: "P",
		ϡlstackID: "L",
		ϡvstackID: "V",
		ϡistackID: "I",
		ϡastackID: "A",
	}
)

// special values that may be pushed on the V stack.
var ϡvSpecialValues = []interface{}{
	nil,
	ϡmatchFailed,
	[]interface{}(nil),
}

type ϡmemoizedResult struct {
	v  interface{}
	pt ϡsvpt
}

// ϡprogram is the data structure that is generated by the builder
// based on an input PEG. It contains the program information required
// to execute the grammar using the vm.
type ϡprogram struct {
	instrs []ϡinstr

	// lists
	ms []ϡmatcher
	as []func(*ϡvm) (interface{}, error)
	bs []func(*ϡvm) (bool, error)
	ss []string

	// instrToRule is the mapping of an instruction index to a rule
	// identifier (or display name) in the ss list:
	//
	// ss[instrToRule[instrIndex]] == name of the rule
	//
	// Since instructions are limited to 65535, the size of this slice
	// is bounded.
	instrToRule []int
}

// String formats the program's instructions in a human-readable format.
func (pg ϡprogram) String() string {
	var buf bytes.Buffer
	var n int

	for i, instr := range pg.instrs {
		if n > 0 {
			n -= 4
			continue
		}
		_, n, _, _, _ = instr.decode()
		n -= 3

		buf.WriteString(fmt.Sprintf("[%3d]: %s\n", i, pg.instrToString(instr, i)))
	}
	return buf.String()
}

// instrToString formats an instruction in a human-readable format, in the
// context of the program.
func (pg ϡprogram) instrToString(instr ϡinstr, ix int) string {
	var buf bytes.Buffer

	op, n, a0, a1, a2 := instr.decode()
	rule := pg.ruleNameAt(ix)
	if rule == "" {
		rule = "<bootstrap>"
	}
	stdFmt := "%s.%s"
	switch op {
	case ϡopCall, ϡopCumulOrF, ϡopReturn, ϡopExit, ϡopRestore,
		ϡopRestoreIfF, ϡopNilIfF, ϡopNilIfT:
		buf.WriteString(fmt.Sprintf(stdFmt, rule, op))
	case ϡopCallA, ϡopCallB, ϡopJump, ϡopJumpIfT, ϡopJumpIfF, ϡopPopVJumpIfF, ϡopTakeLOrJump:
		buf.WriteString(fmt.Sprintf(stdFmt+" %d", rule, op, a0))
	case ϡopPush:
		buf.WriteString(fmt.Sprintf(stdFmt+" %s %d %d", rule, op, ϡstackNm[a0], a1, a2))
		orin := n
		n -= 3
		for n > 0 {
			ix++
			a0, a1, a2, a3 := pg.instrs[ix].decodeLs()
			n -= 4
			buf.WriteString(fmt.Sprintf(" %d %d %d %d", a0, a1, a2, a3))
		}
		buf.WriteString(fmt.Sprintf(" (n=%d)", orin))
	case ϡopPop:
		buf.WriteString(fmt.Sprintf(stdFmt+" %s", rule, op, ϡstackNm[a0]))
	case ϡopMatch:
		buf.WriteString(fmt.Sprintf(stdFmt+" %d (%s)", rule, op, a0, pg.ms[a0]))
	case ϡopStoreIfT:
		buf.WriteString(fmt.Sprintf(stdFmt+" %d (%s)", rule, op, a0, pg.ss[a0]))
	default:
		buf.WriteString(fmt.Sprintf(stdFmt+" %d %d", rule, op, a0, a1))
	}
	return buf.String()
}

// ruleNameAt returns the name of the rule that contains the instruction
// index. It returns an empty string is the instruction is not part of a
// rule (bootstrap instruction, invalid index).
func (pg ϡprogram) ruleNameAt(instrIx int) string {
	if instrIx < 0 || instrIx >= len(pg.instrToRule) {
		return ""
	}
	ssIx := pg.instrToRule[instrIx]
	if ssIx < 0 || ssIx >= len(pg.ss) {
		return ""
	}
	return pg.ss[ssIx]
}

// ϡvm holds the state to execute a compiled grammar.
type ϡvm struct {
	// input
	filename string
	parser   *ϡparser

	// options
	debug   bool
	memoize bool
	recover bool
	// TODO : no bounds checking option (for stacks)? benchmark to see if it's worth it.

	// program data
	pc  int
	pg  *ϡprogram
	cur current

	// stacks
	p ϡpstack
	l ϡlstack
	v ϡvstack
	i ϡistack
	a ϡastack

	// TODO: memoization...
	// TODO: farthest failure position

	// error list
	errs errList
}

// setOptions applies the options in sequence on the vm. It returns the
// vm to allow for chaining calls.
func (v *ϡvm) setOptions(opts []Option) *ϡvm {
	for _, opt := range opts {
		opt(v)
	}
	return v
}

// addErr adds the error at the current parser position, without rule name
// information.
func (v *ϡvm) addErr(err error) {
	v.addErrAt(err, -1, v.parser.pt.position)
}

// addErrAt adds the error at the specified position, for the instruction
// at instrIx.
func (v *ϡvm) addErrAt(err error, instrIx int, pos position) {
	var buf bytes.Buffer
	if v.filename != "" {
		buf.WriteString(v.filename)
	}
	if buf.Len() > 0 {
		buf.WriteString(":")
	}
	buf.WriteString(fmt.Sprintf("%s", pos))

	ruleNm := v.pg.ruleNameAt(instrIx)
	if ruleNm != "" {
		buf.WriteString(": ")
		buf.WriteString("rule " + ruleNm)
	}

	pe := &parserError{Inner: err, ϡprefix: buf.String()}
	v.errs.ϡadd(pe)
}

// dumpSnapshot writes a dump of the current VM state to w.
func (v *ϡvm) dumpSnapshot(w io.Writer) {
	var buf bytes.Buffer

	if v.filename != "" {
		buf.WriteString(v.filename + ":")
	}
	buf.WriteString(fmt.Sprintf("%s: %#U\n", v.parser.pt.position, v.parser.pt.rn))

	// write the next 5 instructions
	ix := v.pc - 1
	if ix > 0 {
		ix--
	}
	stdFmt := ". [%d]: %s"
	for i := 0; i < 5; i++ {
		stdFmt := stdFmt
		if ix == v.pc-1 {
			stdFmt = ">" + stdFmt[1:]
		}
		instr := v.pg.instrs[ix]
		op, n, _, _, _ := instr.decode()
		switch op {
		case ϡopCall:
			buf.WriteString(fmt.Sprintf(stdFmt+"\n", ix, v.pg.instrToString(instr, ix)))
			ix = v.i.pop() // continue with instructions at this index
			v.i.push(ix)
			continue
		default:
			buf.WriteString(fmt.Sprintf(stdFmt+"\n", ix, v.pg.instrToString(instr, ix)))
		}
		ix++
		n -= 3
		for n > 0 {
			ix++
			n -= 4
		}
		if ix >= len(v.pg.instrs) {
			break
		}
	}

	// print the stacks
	buf.WriteString("[ P: ")
	for i := 0; i < 3; i++ {
		if len(v.p) <= i {
			break
		}
		if i > 0 {
			buf.WriteString(", ")
		}
		val := v.p[len(v.p)-i-1]
		buf.WriteString(fmt.Sprintf("\"%v\"", val))
	}
	buf.WriteString(" ]\n[ V: ")
	for i := 0; i < 3; i++ {
		if len(v.v) <= i {
			break
		}
		if i > 0 {
			buf.WriteString(", ")
		}
		val := v.v[len(v.v)-i-1]
		buf.WriteString(fmt.Sprintf("%#v", val))
	}
	buf.WriteString(" ]\n[ I: ")
	for i := 0; i < 3; i++ {
		if len(v.i) <= i {
			break
		}
		if i > 0 {
			buf.WriteString(", ")
		}
		val := v.i[len(v.i)-i-1]
		buf.WriteString(fmt.Sprintf("%d", val))
	}
	buf.WriteString(" ]\n[ L: ")
	for i := 0; i < 3; i++ {
		if len(v.l) <= i {
			break
		}
		if i > 0 {
			buf.WriteString(", ")
		}
		val := v.l[len(v.l)-i-1]
		buf.WriteString(fmt.Sprintf("%v", val))
	}
	buf.WriteString(" ]\n")
	fmt.Fprintln(w, buf.String())
}

// run executes the provided program in this VM, and returns the result.
func (v *ϡvm) run(pg *ϡprogram) (interface{}, error) {
	v.pg = pg
	v.a = make(ϡastack, 0, 128)
	v.i = make(ϡistack, 0, 128)
	v.v = make(ϡvstack, 0, 128)
	v.l = make(ϡlstack, 0, 128)
	v.p = make(ϡpstack, 0, 128)
	ret := v.dispatch()

	// if the match failed, translate that to a nil result and make
	// sure it returns an error
	if ret == ϡmatchFailed {
		ret = nil
		if len(v.errs) == 0 {
			v.addErr(errNoMatch)
		}
	}

	return ret, v.errs.ϡerr()
}

// dispatch is the proper execution method of the VM, it loops over
// the instructions and executes each opcode.
func (v *ϡvm) dispatch() interface{} {
	var instrPath []int
	if v.debug {
		fmt.Fprintln(os.Stderr, v.pg)
		defer func() {
			var buf bytes.Buffer

			buf.WriteString("Execution path:\n")
			for _, ix := range instrPath {
				buf.WriteString(fmt.Sprintf("[%3d]: %s\n", ix, v.pg.instrToString(v.pg.instrs[ix], ix)))
			}
			fmt.Fprintln(os.Stderr, buf.String())
		}()
	}

	if v.recover {
		defer func() {
			if e := recover(); e != nil {
				switch e := e.(type) {
				case error:
					v.addErrAt(e, v.pc-1, v.parser.pt.position)
				default:
					v.addErrAt(fmt.Errorf("%v", e), v.pc-1, v.parser.pt.position)
				}
			}
		}()
	}

	// move to first rune before starting the loop
	v.parser.read()
	for {
		// fetch and decode the instruction
		instr := v.pg.instrs[v.pc]
		op, n, a0, a1, a2 := instr.decode()
		instrPath = append(instrPath, v.pc)

		// increment program counter
		v.pc++

		switch op {
		case ϡopCall:
			if v.debug {
				v.dumpSnapshot(os.Stderr)
			}
			ix := v.i.pop()
			v.i.push(v.pc)
			v.pc = ix

		case ϡopCallA:
			if v.debug {
				v.dumpSnapshot(os.Stderr)
			}
			v.v.pop()
			start := v.p.pop()
			v.cur.pos = start.position
			v.cur.text = v.parser.sliceFrom(start)
			if a0 >= len(v.pg.as) {
				panic(fmt.Sprintf("invalid %s argument: %d", op, a0))
			}
			fn := v.pg.as[a0]
			val, err := fn(v)
			if err != nil {
				v.addErrAt(err, v.pc-1, start.position)
			}
			v.v.push(val)

		case ϡopCallB:
			if v.debug {
				v.dumpSnapshot(os.Stderr)
			}
			v.cur.pos = v.parser.pt.position
			v.cur.text = nil
			if a0 >= len(v.pg.bs) {
				panic(fmt.Sprintf("invalid %s argument: %d", op, a0))
			}
			fn := v.pg.bs[a0]
			val, err := fn(v)
			if err != nil {
				v.addErrAt(err, v.pc-1, v.parser.pt.position)
			}
			if !val {
				v.v.push(ϡmatchFailed)
				break
			}
			v.v.push(nil)

		case ϡopCumulOrF:
			va, vb := v.v.pop(), v.v.pop()
			if va == ϡmatchFailed {
				v.v.push(ϡmatchFailed)
				break
			}
			switch vb := vb.(type) {
			case []interface{}:
				vb = append(vb, va)
				v.v.push(vb)
			case ϡsentinel:
				v.v.push([]interface{}{va})
			default:
				panic(fmt.Sprintf("invalid %s value type on the V stack: %T", op, vb))
			}

		case ϡopExit:
			return v.v.pop()

		case ϡopNilIfF:
			if top := v.v.pop(); top == ϡmatchFailed {
				v.v.push(nil)
				break
			}
			v.v.push(ϡmatchFailed)

		case ϡopNilIfT:
			if top := v.v.pop(); top != ϡmatchFailed {
				v.v.push(nil)
				break
			}
			v.v.push(ϡmatchFailed)

		case ϡopJump:
			v.pc = a0

		case ϡopJumpIfF:
			if top := v.v.peek(); top == ϡmatchFailed {
				v.pc = a0
			}

		case ϡopJumpIfT:
			if top := v.v.peek(); top != ϡmatchFailed {
				v.pc = a0
			}

		case ϡopMatch:
			start := v.parser.pt
			if a0 >= len(v.pg.ms) {
				panic(fmt.Sprintf("invalid %s argument: %d", op, a0))
			}
			m := v.pg.ms[a0]
			if ok := m.match(v.parser); ok {
				v.v.push(v.parser.sliceFrom(start))
				break
			}
			v.v.push(ϡmatchFailed)
			v.parser.pt = start

			if v.debug {
				v.dumpSnapshot(os.Stderr)
			}

		case ϡopPop:
			switch a0 {
			case ϡlstackID:
				v.l.pop()
			case ϡpstackID:
				v.p.pop()
			case ϡastackID:
				v.a.pop()
			case ϡvstackID:
				v.v.pop()
			default:
				panic(fmt.Sprintf("invalid %s argument: %d", op, a0))
			}

		case ϡopPopVJumpIfF:
			if top := v.v.peek(); top == ϡmatchFailed {
				v.v.pop()
				v.pc = a0
			}

		case ϡopPush:
			switch a0 {
			case ϡpstackID:
				v.p.push(v.parser.pt)
			case ϡistackID:
				v.i.push(a1)
			case ϡvstackID:
				if a1 >= len(ϡvSpecialValues) {
					panic(fmt.Sprintf("invalid %s V stack argument: %d", op, a1))
				}
				v.v.push(ϡvSpecialValues[a1])
			case ϡastackID:
				v.a.push()
			case ϡlstackID:
				// n = L args to push + 1, for the lstackID
				n--
				ar := make([]int, n)
				src := []int{a1, a2}
				n -= 2
				for n > 0 {
					// need more
					instr := v.pg.instrs[v.pc]
					a0, a1, a2, a3 := instr.decodeLs()
					src = append(src, a0, a1, a2, a3)
					v.pc++
					n -= 4
				}
				copy(ar, src)
				v.l.push(ar)
			default:
				panic(fmt.Sprintf("invalid %s argument: %d", op, a0))
			}

		case ϡopRestore:
			pt := v.p.pop()
			v.parser.pt = pt

		case ϡopRestoreIfF:
			pt := v.p.pop()
			if top := v.v.peek(); top == ϡmatchFailed {
				v.parser.pt = pt
			}

		case ϡopReturn:
			ix := v.i.pop()
			v.pc = ix

		case ϡopStoreIfT:
			if top := v.v.peek(); top != ϡmatchFailed {
				// get the label name
				if a0 >= len(v.pg.ss) {
					panic(fmt.Sprintf("invalid %s argument: %d", op, a0))
				}
				lbl := v.pg.ss[a0]

				// store the value
				as := v.a.peek()
				as[lbl] = top
			}

		case ϡopTakeLOrJump:
			ix := v.l.take()
			if ix < 0 {
				v.pc = a0
				break
			}
			v.i.push(ix)

		default:
			panic(fmt.Sprintf("unknown opcode %s", op))
		}
	}
}
`
