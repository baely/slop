package main

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
)

type WatchedEntry struct {
	Date, Name, Year, URI string
}

type DiaryEntry struct {
	Date, Name, Year, URI, Rating, Rewatch, Tags, WatchedDate string
}

type ReviewEntry struct {
	Date, Name, Year, URI, Rating, Rewatch, Review, Tags, WatchedDate string
}

type WatchlistEntry struct {
	Date, Name, Year, URI string
}

type LBData struct {
	Profile   []byte
	Watched   []WatchedEntry
	Diary     []DiaryEntry
	Ratings   map[string]string // URI -> rating
	Reviews   []ReviewEntry
	Watchlist []WatchlistEntry
	LikedURIs map[string]bool
}

func ParseLetterboxdZip(r io.ReaderAt, size int64) (*LBData, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}

	files := map[string][]byte{}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		// Skip deleted/orphaned trees entirely
		if strings.HasPrefix(f.Name, "deleted/") || strings.HasPrefix(f.Name, "orphaned/") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", f.Name, err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", f.Name, err)
		}
		files[f.Name] = b
	}

	data := &LBData{
		Profile:   files["profile.csv"],
		Ratings:   map[string]string{},
		LikedURIs: map[string]bool{},
	}

	if b, ok := files["watched.csv"]; ok {
		rows, err := readCSV(b)
		if err != nil {
			return nil, fmt.Errorf("watched.csv: %w", err)
		}
		for _, row := range rows {
			data.Watched = append(data.Watched, WatchedEntry{
				Date: get(row, "Date"),
				Name: get(row, "Name"),
				Year: get(row, "Year"),
				URI:  get(row, "Letterboxd URI"),
			})
		}
	}
	if b, ok := files["diary.csv"]; ok {
		rows, err := readCSV(b)
		if err != nil {
			return nil, fmt.Errorf("diary.csv: %w", err)
		}
		for _, row := range rows {
			data.Diary = append(data.Diary, DiaryEntry{
				Date:        get(row, "Date"),
				Name:        get(row, "Name"),
				Year:        get(row, "Year"),
				URI:         get(row, "Letterboxd URI"),
				Rating:      get(row, "Rating"),
				Rewatch:     get(row, "Rewatch"),
				Tags:        get(row, "Tags"),
				WatchedDate: get(row, "Watched Date"),
			})
		}
	}
	if b, ok := files["ratings.csv"]; ok {
		rows, err := readCSV(b)
		if err != nil {
			return nil, fmt.Errorf("ratings.csv: %w", err)
		}
		for _, row := range rows {
			uri := get(row, "Letterboxd URI")
			if uri != "" {
				data.Ratings[uri] = get(row, "Rating")
			}
		}
	}
	if b, ok := files["reviews.csv"]; ok {
		rows, err := readCSV(b)
		if err != nil {
			return nil, fmt.Errorf("reviews.csv: %w", err)
		}
		for _, row := range rows {
			data.Reviews = append(data.Reviews, ReviewEntry{
				Date:        get(row, "Date"),
				Name:        get(row, "Name"),
				Year:        get(row, "Year"),
				URI:         get(row, "Letterboxd URI"),
				Rating:      get(row, "Rating"),
				Rewatch:     get(row, "Rewatch"),
				Review:      get(row, "Review"),
				Tags:        get(row, "Tags"),
				WatchedDate: get(row, "Watched Date"),
			})
		}
	}
	if b, ok := files["watchlist.csv"]; ok {
		rows, err := readCSV(b)
		if err != nil {
			return nil, fmt.Errorf("watchlist.csv: %w", err)
		}
		for _, row := range rows {
			data.Watchlist = append(data.Watchlist, WatchlistEntry{
				Date: get(row, "Date"),
				Name: get(row, "Name"),
				Year: get(row, "Year"),
				URI:  get(row, "Letterboxd URI"),
			})
		}
	}
	if b, ok := files["likes/films.csv"]; ok {
		rows, err := readCSV(b)
		if err != nil {
			return nil, fmt.Errorf("likes/films.csv: %w", err)
		}
		for _, row := range rows {
			uri := get(row, "Letterboxd URI")
			if uri != "" {
				data.LikedURIs[uri] = true
			}
		}
	}
	return data, nil
}

// readCSV returns each row as a map keyed by header name.
func readCSV(b []byte) ([]map[string]string, error) {
	r := csv.NewReader(bytes.NewReader(b))
	r.FieldsPerRecord = -1
	header, err := r.Read()
	if err == io.EOF {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []map[string]string
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		row := map[string]string{}
		for i, h := range header {
			if i < len(rec) {
				row[h] = rec[i]
			}
		}
		out = append(out, row)
	}
	return out, nil
}

func get(m map[string]string, key string) string {
	return m[key]
}
