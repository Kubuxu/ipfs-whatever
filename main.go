package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math"
	"os"
	"text/tabwriter"
	"time"

	randbo "github.com/dustin/randbo"
	api "github.com/ipfs/go-ipfs-api"

	fsrepo "github.com/ipfs/go-ipfs/repo/fsrepo"

	cli "github.com/codegangsta/cli"
)

var sh *api.Shell

func checkPatchOpsPerSec(count int) (float64, error) {
	r := randbo.New()
	basedata := make([]byte, 100)
	r.Read(basedata)
	base := "QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn"

	cur, err := sh.PatchData(base, true, basedata)
	if err != nil {
		log.Fatal(err)
	}

	before := time.Now()
	for i := 0; i < count; i++ {
		out, err := sh.PatchLink(base, "next-link-in-chain", cur, false)
		if err != nil {
			fmt.Println("error: ", base, cur)
			return 0, err
		}

		cur = out
	}
	took := time.Now().Sub(before)

	return float64(count) / took.Seconds(), nil
}

func checkAddLink(count int) (float64, float64, error) {
	var times []float64

	r := randbo.New()
	basedata := make([]byte, 100)
	r.Read(basedata)
	base := "QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn"

	prev := base
	for i := 0; i < count; i++ {
		start := time.Now()

		cur, err := sh.PatchLink(base, "FIRST", prev, false)
		if err != nil {
			fmt.Println("error: ", err)
			return 0, 0, err
		}

		for j := 0; j < 200; j++ {
			out, err := sh.PatchLink(cur, fmt.Sprintf("link-%d", j), base, false)
			if err != nil {
				fmt.Println("error: ", base, cur)
				return 0, 0, err
			}

			cur = out
		}
		took := float64(time.Now().Sub(start)) / 200 / float64(time.Second)
		times = append(times, 1/took)
	}
	avg, stdev := timeStats(times)
	return avg, stdev, nil
}

func checkAddFile(size int) (float64, float64, error) {
	trials := 15
	buf := new(bytes.Buffer)
	var times []float64

	for i := 0; i < trials; i++ {
		io.CopyN(buf, randbo.New(), int64(size))

		start := time.Now()
		_, err := sh.Add(buf)
		if err != nil {
			return 0, 0, err
		}
		took := float64(time.Now().Sub(start)) / float64(time.Second)
		times = append(times, took)
	}

	av, stdev := timeStats(times)
	return av, stdev, nil
}

func timeStats(ts []float64) (float64, float64) {
	var average float64
	for _, d := range ts {
		average += d / float64(len(ts))
	}

	var stdevsum float64
	for _, d := range ts {
		v := average - d
		stdevsum += (v * v)
	}

	stdev := math.Sqrt(stdevsum / float64(len(ts)))

	return average, stdev
}

type IpfsBenchStats struct {
	PatchOpsPerSec  float64
	Add10MBTime     float64
	Add10MBStdev    float64
	DirAddOpsPerSec float64
	DirAddOpsStdev  float64
}

func getShell() error {
	rpath, err := fsrepo.BestKnownPath()
	if err != nil {
		return err
	}

	apiaddr, err := fsrepo.APIAddr(rpath)
	if err != nil {
		return err
	}

	sh = api.NewShell(apiaddr)
	return nil
}

func runBenchmarks() (*IpfsBenchStats, error) {
	stats := new(IpfsBenchStats)

	fmt.Fprintln(os.Stderr, "checking patch operations per second...")
	count, err := checkPatchOpsPerSec(1500)
	if err != nil {
		return nil, err
	}
	stats.PatchOpsPerSec = count

	fmt.Fprintln(os.Stderr, "checking 10MB file adds...")
	av, stdev, err := checkAddFile(10 * 1024 * 1024)
	if err != nil {
		return nil, err
	}
	stats.Add10MBTime = av
	stats.Add10MBStdev = stdev

	fmt.Fprintln(os.Stderr, "checking add-link ops per second...")
	diradd, diraddstd, err := checkAddLink(100)
	if err != nil {
		return nil, err
	}
	stats.DirAddOpsPerSec = diradd
	stats.DirAddOpsStdev = diraddstd
	return stats, nil
}

func main() {
	app := cli.NewApp()
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "before",
			Usage: "specify file containing results from previous run to benchmark against",
		},
	}
	app.Action = func(c *cli.Context) error {
		err := getShell()
		if err != nil {
			return err
		}

		var oldstats *IpfsBenchStats
		if bef := c.String("before"); bef != "" {
			oldstats = new(IpfsBenchStats)
			data, err := ioutil.ReadFile(bef)
			if err != nil {
				return err
			}

			err = json.Unmarshal(data, oldstats)
			if err != nil {
				return err
			}
		}

		nstats, err := runBenchmarks()
		if err != nil {
			return err
		}

		if oldstats == nil {
			return json.NewEncoder(os.Stdout).Encode(nstats)
		}

		printBenchResults(oldstats, nstats)
		return nil
	}
	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func writeStat(w io.Writer, name string, before, after float64) {
	delta := 100 * ((after / before) - 1)
	fmt.Fprintf(w, "%s\t%.2f\t%.2f\t%.2f%%\n", name, before, after, delta)
}

func printBenchResults(a, b *IpfsBenchStats) {
	fmt.Println()
	w := tabwriter.NewWriter(os.Stdout, 4, 4, 2, ' ', 0)
	fmt.Fprintln(w, "Results\tBefore\tAfter\t% Change")
	writeStat(w, "PatchOpsPerSec", a.PatchOpsPerSec, b.PatchOpsPerSec)
	writeStat(w, "DirAddOpsPerSec", a.DirAddOpsPerSec, b.DirAddOpsPerSec)
	writeStat(w, "DirAddOpsStdev", a.DirAddOpsStdev, b.DirAddOpsStdev)
	writeStat(w, "Add10MBTime", a.Add10MBTime, b.Add10MBTime)
	writeStat(w, "Add10MBStdev", a.Add10MBStdev, b.Add10MBStdev)
	w.Flush()
}
