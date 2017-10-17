package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/gizak/termui"
)

const statFilePath = "/proc/stat"
const meminfoFilePath = "/proc/meminfo"
const netinfoFilePath = "/proc/net/dev"

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}

type ProcessList struct {
}

type CpuStat struct {
	user   float32
	nice   float32
	system float32
	idle   float32
}

type CpusStats struct {
	stat map[string]CpuStat
	proc map[string]CpuStat
}

func NewCpusStats(s map[string]CpuStat) *CpusStats {
	return &CpusStats{stat: s, proc: make(map[string]CpuStat)}
}

func (cs *CpusStats) String() (ret string) {
	for key, _ := range cs.proc {
		ret += fmt.Sprintf("%s: %.2f %.2f %.2f %.2f\n", key, cs.proc[key].user, cs.proc[key].nice, cs.proc[key].system, cs.proc[key].idle)
	}
	return
}

func subCpuStat(m CpuStat, s CpuStat) CpuStat {
	return CpuStat{user: m.user - s.user,
		nice:   m.nice - s.nice,
		system: m.system - s.system,
		idle:   m.idle - s.idle}
}

func procCpuStat(c CpuStat) CpuStat {
	sum := c.user + c.nice + c.system + c.idle
	return CpuStat{user: c.user / sum * 100,
		nice:   c.nice / sum * 100,
		system: c.system / sum * 100,
		idle:   c.idle / sum * 100}
}

func (cs *CpusStats) tick(ns map[string]CpuStat) {
	for key, _ := range cs.stat {
		proc := subCpuStat(ns[key], cs.stat[key])
		cs.proc[key] = procCpuStat(proc)
		cs.stat[key] = ns[key]
	}
}

type CpuChart struct {
	Gauge  *termui.Gauge
	LChart *termui.LineChart
}

func NewCpuChart(width int) *CpuChart {
	cg := termui.NewGauge()
	cg.Width = width
	cg.Height = 3
	cg.Percent = 0
	cg.BorderLabel = "CPU UTILIZATION"

	lc := termui.NewLineChart()
	lc.Width = width
	lc.Height = 12
	lc.X = 0
	lc.Mode = "dot"
	lc.BorderLabel = "CPU"
	lc.LineColor = termui.ColorCyan
	return &CpuChart{Gauge: cg, LChart: lc}
}

func (cc *CpuChart) Update(cs CpusStats) {
	for key, val := range cs.proc {
		if key == "cpu" {
			p := int(val.user + val.nice + val.system)
			cc.Gauge.Percent = p
			cc.LChart.Data = append(cc.LChart.Data, 0)
			copy(cc.LChart.Data[1:], cc.LChart.Data[0:])
			cc.LChart.Data[0] = float64(p)
		}
	}
}

type MemChart struct {
	Gauge  *termui.Gauge
	SLines *termui.Sparklines
}

func NewMemChart(width int) *MemChart {

	g := termui.NewGauge()
	g.Width = width
	g.Height = 3
	g.BorderLabel = "MEMORY UTILIZATION"

	sline := termui.NewSparkline()
	sline.Title = "MEM"
	sline.Height = 8
	sline.LineColor = termui.ColorGreen

	sls := termui.NewSparklines(sline)
	sls.Width = width
	sls.Height = 12
	sls.Y = 3
	return &MemChart{Gauge: g, SLines: sls}
}

func (mte *MemChart) Update(ms MemStat) {
	used := int((ms.total - ms.free) * 100 / ms.total)
	mte.Gauge.Percent = used
	mte.SLines.Lines[0].Data = append(mte.SLines.Lines[0].Data, 0)
	copy(mte.SLines.Lines[0].Data[1:], mte.SLines.Lines[0].Data[0:])
	mte.SLines.Lines[0].Data[0] = used
	if len(mte.SLines.Lines[0].Data) > mte.SLines.Width-2 {
		mte.SLines.Lines[0].Data = mte.SLines.Lines[0].Data[0 : mte.SLines.Width-2]
	}
}

type NetChart struct {
	RxLines termui.Sparkline
	TxLines termui.Sparkline
	GLines  *termui.Sparklines
	txTotal int64
	rxTotal int64
}

func NewNetChart(width int) *NetChart {
	r := termui.NewSparkline()
	//r.Width = width
	r.Height = 4
	r.Title = "Rx"
	r.LineColor = termui.ColorBlue

	t := termui.NewSparkline()
	//t.Width = width
	t.Height = 4
	t.Title = "Tx"
	t.LineColor = termui.ColorRed

	g := termui.NewSparklines(r, t)
	g.Width = width
	g.Height = 12
	g.BorderLabel = "NETWORK"

	return &NetChart{TxLines: t, RxLines: r, GLines: g}
}

func (nc *NetChart) Update(ns NetStat) {
	var r, t int
	if nc.txTotal != 0 {
		t = int(ns.txTotal - nc.txTotal)
	}
	if nc.rxTotal != 0 {
		r = int(ns.rxTotal - nc.rxTotal)
	}
	nc.rxTotal = ns.rxTotal
	nc.txTotal = ns.txTotal

	nc.GLines.Lines[0].Data = append(nc.GLines.Lines[0].Data, 0)
	copy(nc.GLines.Lines[0].Data[1:], nc.GLines.Lines[0].Data[0:])
	nc.GLines.Lines[0].Data[0] = r
	nc.GLines.Lines[0].Title = "Rx " + formatRate(r)
	if len(nc.GLines.Lines[0].Data) > nc.GLines.Width-2 {
		nc.GLines.Lines[0].Data = nc.GLines.Lines[0].Data[0 : nc.GLines.Width-2]
	}

	nc.GLines.Lines[1].Data = append(nc.GLines.Lines[1].Data, 0)
	copy(nc.GLines.Lines[1].Data[1:], nc.GLines.Lines[1].Data[0:])
	nc.GLines.Lines[1].Data[0] = t
	nc.GLines.Lines[1].Title = "Tx " + formatRate(t)
	if len(nc.GLines.Lines[1].Data) > nc.GLines.Width-2 {
		nc.GLines.Lines[1].Data = nc.GLines.Lines[1].Data[0 : nc.GLines.Width-2]
	}
}

func formatRate(bytes int) (ret string) {
	num := 0
	unit := "Byte"
	if bytes < 1024 {
		num = bytes
		unit = "Byte"
	} else if bytes < 1024*1024 {
		num = bytes / 1024
		unit = "KB"
	} else if bytes < 1024*1024*1024 {
		num = bytes / (1024 * 1024)
		unit = "MB"
	} else if bytes < 1024*1024*1024*1024 {
		num = bytes / (1024 * 1024 * 1024)
		unit = "GB"
	} else {
		num = bytes / (1024 * 1024 * 1024 * 1024)
		unit = "TB"
	}

	ret = fmt.Sprintf("%d %s/s", num, unit)
	return
}

type processInfo struct {
	pid  int64
	user string
	comm string
	pcpu float64
	pmem float64

	txTotal float64
	rxTotal float64

	wdRate  float64
	rdRate  float64
	wdTotal float64
	rdTotal float64
}

func (p *processInfo) refreshNet() (err error) {
	//var b []byte
	//if b, err = readFile(fmt.Sprintf("/proc/")); err != nil {
	//return
	//}

	return
}

func (p *processInfo) refresh() (err error) {

	out, err := exec.Command("ps", "--pid", fmt.Sprint(p.pid), "-opid,%cpu,%mem,user,comm", "--sort=-pcpu").Output()
	if err != nil {
		log.Println(err)
		return
	}

	outString := string(out)
	lines := strings.Split(outString, "\n")
	log.Println(len(lines))
	if len(lines) < 2 {
		err = errors.New("Dead Pid")
		return
	}
	return
}

func (p *processInfo) refreshIO() (err error) {
	var b []byte
	if b, err = readFile(fmt.Sprintf("/proc/%d/io", p.pid)); err != nil {
		return
	}

	outString := string(b)
	lines := strings.Split(outString, "\n")

	var rb, wb float64
	for _, line := range lines {
		if line == "" {
			continue
		}
		v := strings.Split(line, " ")[1]
		if strings.Contains(line, "read_bytes") {
			rb, _ = strconv.ParseFloat(v, 64)
		} else if strings.Contains(line, "write_bytes") {
			wb, _ = strconv.ParseFloat(v, 64)
		}
	}

	if p.wdTotal != 0 && p.rdTotal != 0 {
		p.wdRate = wb - p.wdTotal
		p.rdRate = rb - p.rdTotal
	}

	p.wdTotal = wb
	p.rdTotal = rb

	return
}

func fetchAllProcess() (processList []processInfo, err error) {
	processList = make([]processInfo, 0)

	out, err := exec.Command("ps", "-e", "-opid,%cpu,%mem,user,comm", "--sort=-pcpu").Output()
	if err != nil {
		log.Println(err)
		return
	}

	outString := string(out)
	lines := strings.Split(outString, "\n")

	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		lineSplits := strings.Fields(line)

		pid, _ := strconv.ParseInt(lineSplits[0], 0, 64)
		pcpu, _ := strconv.ParseFloat(lineSplits[1], 64)
		pmem, _ := strconv.ParseFloat(lineSplits[2], 64)
		user := lineSplits[3]
		comm := lineSplits[4]

		p := processInfo{
			pid:  pid,
			user: user,
			comm: comm,
			pcpu: pcpu,
			pmem: pmem,
		}

		p.refreshIO()
		processList = append(processList, p)
	}

	return
}

func readFile(filename string) (b []byte, err error) {
	var f *os.File
	if f, err = os.Open(filename); err != nil {
		return
	}
	defer f.Close()

	buf := make([]byte, 1024)
	var n int
	if n, err = f.Read(buf); err != nil {
		return
	}

	b = buf[:n]
	return
}

func refreshList() (pidList, commList, userList, cpuList, memList, dioList []string) {

	pList, _ := fetchAllProcess()
	for _, p := range pList {
		pidList = append(pidList, fmt.Sprint(p.pid))
		commList = append(commList, p.comm)
		userList = append(userList, p.user)
		cpuList = append(cpuList, fmt.Sprint(p.pcpu))
		memList = append(memList, fmt.Sprint(p.pmem))
		dioList = append(dioList, fmt.Sprintf("r: %f B/s w: %f B/s", p.rdRate, p.wdRate))
	}

	return
}

type errIntParser struct {
	err error
}

func (eip *errIntParser) parse(s string) (ret int64) {
	if eip.err != nil {
		return 0
	}
	ret, eip.err = strconv.ParseInt(s, 10, 0)
	return
}

type LineProcessor interface {
	process(string) error
	finalize() interface{}
}

type CpuLineProcessor struct {
	m map[string]CpuStat
}

func (clp *CpuLineProcessor) process(line string) (err error) {
	r := regexp.MustCompile("^cpu([0-9]*)")

	if r.MatchString(line) {
		tab := strings.Fields(line)
		if len(tab) < 5 {
			err = errors.New("cpu info line has not enough fields")
			return
		}
		parser := errIntParser{}
		cs := CpuStat{user: float32(parser.parse(tab[1])),
			nice:   float32(parser.parse(tab[2])),
			system: float32(parser.parse(tab[3])),
			idle:   float32(parser.parse(tab[4]))}
		clp.m[tab[0]] = cs
		err = parser.err
		if err != nil {
			return
		}
	}
	return
}

func (clp *CpuLineProcessor) finalize() interface{} {
	return clp.m
}

type MemStat struct {
	total int64
	free  int64
}

func (ms MemStat) String() (ret string) {
	ret = fmt.Sprintf("TotalMem: %d, FreeMem: %d\n", ms.total, ms.free)
	return
}

func (ms *MemStat) process(line string) (err error) {
	rtotal := regexp.MustCompile("^MemTotal:")
	rfree := regexp.MustCompile("^MemFree:")
	var aux int64
	if rtotal.MatchString(line) || rfree.MatchString(line) {
		tab := strings.Fields(line)
		if len(tab) < 3 {
			err = errors.New("mem info line has not enough fields")
			return
		}
		aux, err = strconv.ParseInt(tab[1], 10, 0)
	}
	if err != nil {
		return
	}

	if rtotal.MatchString(line) {
		ms.total = aux
	}
	if rfree.MatchString(line) {
		ms.free = aux
	}
	return
}

func (ms *MemStat) finalize() interface{} {
	return *ms
}

type NetStat struct {
	txTotal int64
	rxTotal int64
}

func (ns *NetStat) String() (ret string) {
	ret = fmt.Sprintf("RxTotal: %d, TxTotal: %d\n", ns.rxTotal, ns.txTotal)
	return
}

func (ns *NetStat) process(line string) (err error) {
	if strings.Contains(line, ":") && !strings.Contains(line, "lo:") {
		tab := strings.Fields(line)
		var rx, tx int64
		rx, err = strconv.ParseInt(tab[1], 10, 0)
		tx, err = strconv.ParseInt(tab[9], 10, 0)
		ns.rxTotal += rx
		ns.txTotal += tx
	}
	return
}

func (ns *NetStat) finalize() interface{} {
	return *ns
}

func processFileLines(filePath string, lp LineProcessor) (ret interface{}, err error) {
	var statFile *os.File
	statFile, err = os.Open(filePath)
	if err != nil {
		fmt.Printf("open: %v\n", err)
	}
	defer statFile.Close()

	statFileReader := bufio.NewReader(statFile)

	for {
		var line string
		line, err = statFileReader.ReadString('\n')
		if err == io.EOF {
			err = nil
			break
		}
		if err != nil {
			fmt.Printf("open: %v\n", err)
			break
		}
		line = strings.TrimSpace(line)

		err = lp.process(line)
	}

	ret = lp.finalize()
	return
}

func getCpusStatsMap() (m map[string]CpuStat, err error) {
	var aux interface{}
	aux, err = processFileLines(statFilePath, &CpuLineProcessor{m: make(map[string]CpuStat)})
	return aux.(map[string]CpuStat), err
}

func getMemStats() (ms MemStat, err error) {
	var aux interface{}
	aux, err = processFileLines(meminfoFilePath, &MemStat{})
	return aux.(MemStat), err
}

func getNetStats() (ns NetStat, err error) {
	var aux interface{}
	aux, err = processFileLines(netinfoFilePath, &NetStat{})
	return aux.(NetStat), err
}

func runUI() {
	if err := termui.Init(); err != nil {
		panic(err)
	}
	defer termui.Close()

	header := buildHeader()
	pidList, commList, userList, cpuList, memList, _ := refreshList()
	pidCol := buildList(pidList)
	commCol := buildList(commList)
	userCol := buildList(userList)
	cpuCol := buildList(cpuList)
	memCol := buildList(memList)

	cs, _ := getCpusStatsMap()
	cpusStats := NewCpusStats(cs)

	memChart := NewMemChart(termui.TermWidth() / 3)
	ms, _ := getMemStats()
	memChart.Update(ms)

	cpuChart := NewCpuChart(termui.TermWidth() / 3)

	netChart := NewNetChart(termui.TermWidth() / 3)
	ns, _ := getNetStats()
	netChart.Update(ns)

	termui.Body.AddRows(
		termui.NewRow(
			termui.NewCol(6, 0, cpuChart.Gauge),
			termui.NewCol(6, 0, memChart.Gauge),
		),
		termui.NewRow(
			termui.NewCol(4, 0, cpuChart.LChart),
			termui.NewCol(4, 0, memChart.SLines),
			termui.NewCol(4, 0, netChart.GLines),
		),
		header,
		termui.NewRow(
			termui.NewCol(2, 0, pidCol),
			termui.NewCol(2, 0, userCol),
			termui.NewCol(2, 0, cpuCol),
			termui.NewCol(2, 0, memCol),
			termui.NewCol(2, 0, commCol),
		),
	)

	termui.Body.Align()

	termui.Render(termui.Body)

	// handle key q pressing
	termui.Handle("/sys/kbd/q", func(termui.Event) {
		// press q to quit
		termui.StopLoop()
	})

	termui.Handle("/timer/1s", func(e termui.Event) {

		//t := e.Data.(termui.EvtTimer)
		//i := t.Count
		//if i > 103 {
		//termui.StopLoop()
		//return
		//}

		pidList, commList, userList, cpuList, memList, _ := refreshList()
		pidCol.Items = pidList
		commCol.Items = commList
		userCol.Items = userList
		cpuCol.Items = cpuList
		memCol.Items = memList

		cs, errcs := getCpusStatsMap()
		if errcs != nil {
			panic(errcs)
		}
		cpusStats.tick(cs)
		cpuChart.Update(*cpusStats)

		ms, errm := getMemStats()
		if errm != nil {
			panic(errm)
		}
		memChart.Update(ms)

		ns, _ := getNetStats()
		netChart.Update(ns)

		termui.Render(termui.Body)
	})

	termui.Handle("/sys/wnd/resize", func(e termui.Event) {
		termui.Body.Width = termui.TermWidth()
		termui.Body.Align()
		termui.Clear()
		termui.Render(termui.Body)
	})

	termui.Loop()
}

func buildHeader() *termui.Row {
	titles := []string{"PID", "USER", "%CPU", "%MEM", "COMMAND"}
	cols := []*termui.Row{}
	for _, title := range titles {
		p := termui.NewPar(title)
		p.Border = false
		p.TextFgColor = termui.ColorYellow
		col := termui.NewCol(2, 0, p)
		cols = append(cols, col)
	}
	return termui.NewRow(cols...)
}

func buildList(dataList []string) *termui.List {
	lst := termui.NewList()

	lst.Items = dataList
	lst.Height = len(dataList)
	lst.Border = false

	return lst
}

func buildPsRows() *termui.Row {
	pidList, commList, userList, cpuList, memList, _ := refreshList()

	lst := [][]string{pidList, commList, userList, cpuList, memList}
	cols := []*termui.Row{}
	for _, itemList := range lst {
		colList := termui.NewList()
		colList.Items = itemList
		colList.Height = len(itemList)
		colList.Border = false
		col := termui.NewCol(1, 0, colList)
		cols = append(cols, col)
	}

	return termui.NewRow(cols...)
}

func main() {
	runUI()
	//ns, err := getNetStats()
	//log.Println(ns, err)
}
