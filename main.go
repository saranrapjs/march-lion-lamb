package main

import (
	"database/sql"
	"html/template"
	"log"
	"os"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Obs struct {
	Rain         float64
	MinTemp      float64
	MaxTemp      float64
	MaxWindSpeed float64
	Date         time.Time
	Future       bool
	IsLion       bool
}

const (
	rainFactor = 2
	tempFactor = 3
	windFactor = 1
)

func (o *Obs) AssignLion(avg *Obs) {
	var total int
	var denominator int
	// was there rain
	switch {
	case o.MaxTemp >= 70.0:
	// if it's in the 70's, it's totally lamb!
	// even if it rained!
		o.IsLion = false
		return
	// if there's anything but a trivial amount of rain,
	// it's LION baby
	case o.Rain > 0.03:
		o.IsLion = true
		return
	case o.Rain > 0:
		total += rainFactor
	}
	denominator += rainFactor
	// what was the high
	switch {
	case o.MaxTemp < 45.0:
		total += 3
		break
	case o.MaxTemp < 50.0:
		total += 2
		break
	}
	denominator += tempFactor
	// was it windy
	if o.MaxWindSpeed > 7 {
		total += windFactor
	}
	denominator += windFactor
	o.IsLion = float64(total)/float64(denominator) >= 0.5
}

const query = `
select
    datetime(archive_day_rain.dateTime, 'unixepoch'),
    archive_day_rain.sum as rain,
    archive_day_outTemp.min as minTemp,
    archive_day_outTemp.max as maxTemp,
    archive_day_windSpeed.max as maxWind
from
    archive_day_rain
left join
    archive_day_outTemp on archive_day_outTemp.dateTime = archive_day_rain.dateTime
left join
    archive_day_windSpeed on archive_day_windSpeed.dateTime = archive_day_rain.dateTime
where
    strftime('%Y-%m', datetime(archive_day_rain.dateTime, 'unixepoch')) = strftime('%Y-03', datetime())
order by
    archive_day_rain.dateTime asc;
`

func predictions(halfwayThruMarch bool, obs []*Obs) (string, string) {
	var inCount, inTotal, outCount, outTotal int
	for _, day := range obs {
		if day.Date.Day() < 16 {
			if day.IsLion {
				inCount++
			}
			inTotal++
		} else {
			if day.IsLion {
				outCount++
			}
			outTotal++
		}
	}
	var inIsLion, outIsLion bool
	if inTotal > 0 {
		inIsLion = (float64(inCount) / float64(inTotal)) > 0.5
	}
	if outTotal > 0 {
		outIsLion = (float64(outCount) / float64(outTotal)) > 0.5
	}
	in := "Lamb"
	if inIsLion {
		in = "Lion"
	}
	out := "TBD"
	switch {
	case halfwayThruMarch && outIsLion:
		out = "Lion"
	case halfwayThruMarch && !outIsLion:
		out = "Lamb"
	}
	return in, out
}

func updateAvg(avg, incoming *Obs, count int) {
	avg.Rain = ((float64(count-1) * avg.Rain) + incoming.Rain) / float64(count)
	avg.MinTemp = ((float64(count-1) * avg.MinTemp) + incoming.MinTemp) / float64(count)
	avg.MaxTemp = ((float64(count-1) * avg.MaxTemp) + incoming.MaxTemp) / float64(count)
	avg.MaxWindSpeed = ((float64(count-1) * avg.MaxWindSpeed) + incoming.MaxWindSpeed) / float64(count)
}

func main() {
	if len(os.Args) < 1 {
		log.Fatal("usage: lion-lamb <path-to-weewx-db>")
	}
	db := os.Args[1]
	if db == "" {
		log.Fatal("usage: lion-lamb <path-to-weewx-db>")
	}
	funcMap := template.FuncMap{
		"ToLower": strings.ToLower,
	}
	loc, _ := time.LoadLocation("America/New_York")
	now := time.Now().In(loc)
	forecasts := []*Obs{}
	database, _ := sql.Open("sqlite3", db+"?parseTime=true")
	rows, err := database.Query(query)
	templateData := map[string]interface{}{}
	templateData["year"] = now.Year()
	if err != nil {
		log.Fatal("could not query: ", err)
	}
	beginningOfMarch := now.Day() < 16 && now.Month() == 3
	var lastRecordedDay int
	var avg Obs
	var count int
	for rows.Next() {
		var t string
		var dt time.Time
		var o Obs
		if err := rows.Scan(&t, &o.Rain, &o.MinTemp, &o.MaxTemp, &o.MaxWindSpeed); err != nil {
			log.Fatal(err)
		}
		dt, _ = time.Parse("2006-01-02 15:04:05", t)
		o.Date = dt
		lastRecordedDay = dt.Day()
		forecasts = append(forecasts, &o)
		count++
		if count == 1 {
			avg = o
		} else {
			updateAvg(&avg, &o, count)
		}
	}
	for _, o := range forecasts {
		o.AssignLion(&avg)
	}
	in, out := predictions(!beginningOfMarch, forecasts)
	templateData["in"] = in
	templateData["out"] = out
	for i := lastRecordedDay + 1; i <= 31; i++ {
		o := Obs{
			Date:   time.Date(time.Now().Year(), time.March, i, 0, 0, 0, 0, time.Local),
			Future: true,
		}
		forecasts = append(forecasts, &o)
	}
	templateData["forecasts"] = forecasts
	theFirst := time.Date(time.Now().Year(), time.March, 1, 0, 0, 0, 0, time.Local)
	templateData["offset"] = int(theFirst.Weekday() + 1)
	templateData["now"] = now
	templateData["tbd"] = beginningOfMarch
	tmpl, _ := template.New("tpl").Funcs(funcMap).ParseFiles("index.tmpl")
	if err := tmpl.ExecuteTemplate(os.Stdout, "index.tmpl", templateData); err != nil {
		log.Fatal(err)
	}
}
