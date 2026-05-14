package providers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/demonkingswarn/luffy/core"
	"github.com/google/uuid"
)

const (
	CINEBOLT_BASE_URL   = "https://cinebolt.net"
	CINEBOLT_API_URL    = CINEBOLT_BASE_URL + "/api"
	CINEBOLT_TMDB_API   = "https://api.themoviedb.org/3"
	CINEBOLT_TMDB_KEY   = "f1dd7f2494de60ef4946ea81fd5ebaba"
	CINEBOLT_USER_AGENT = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

type Cinebolt struct {
	Client *http.Client
}

func NewCinebolt(client *http.Client) *Cinebolt {
	return &Cinebolt{Client: client}
}

func (c *Cinebolt) newTMDBRequest(path string, params url.Values) (*http.Request, error) {
	params.Set("api_key", CINEBOLT_TMDB_KEY)
	fullURL := fmt.Sprintf("%s/%s?%s", CINEBOLT_TMDB_API, path, params.Encode())
	req, err := core.NewRequest("GET", fullURL)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", CINEBOLT_USER_AGENT)
	return req, nil
}

func (c *Cinebolt) newCineboltRequest(method, path string, params url.Values, referer string) (*http.Request, error) {
	fullURL := fmt.Sprintf("%s/%s", CINEBOLT_API_URL, path)
	if len(params) > 0 {
		fullURL += "?" + params.Encode()
	}
	req, err := core.NewRequest(method, fullURL)
	if err != nil {
		return nil, err
	}
	if referer == "" {
		referer = CINEBOLT_BASE_URL + "/"
	}
	req.Header.Set("Referer", referer)
	req.Header.Set("Origin", CINEBOLT_BASE_URL)
	req.Header.Set("User-Agent", CINEBOLT_USER_AGENT)
	return req, nil
}

func (c *Cinebolt) Search(query string) ([]core.SearchResult, error) {
	params := url.Values{}
	params.Set("query", query)
	params.Set("include_adult", "false")
	params.Set("language", "en-US")
	params.Set("page", "1")

	req, err := c.newTMDBRequest("search/multi", params)
	if err != nil {
		return nil, err
	}

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var data core.TmdbSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	var results []core.SearchResult
	for _, r := range data.Results {
		if r.MediaType != "movie" && r.MediaType != "tv" {
			continue
		}

		title := r.Title
		if title == "" {
			title = r.Name
		}

		year := r.ReleaseDate
		if year == "" {
			year = r.FirstAirDate
		}
		if len(year) > 4 {
			year = year[:4]
		}

		mediaType := core.Movie
		if r.MediaType == "tv" {
			mediaType = core.Series
		}

		// Encode more info in URL to carry it through
		results = append(results, core.SearchResult{
			Title:  title,
			URL:    fmt.Sprintf("%s/%s/%d?title=%s&year=%s", CINEBOLT_BASE_URL, r.MediaType, r.ID, url.QueryEscape(title), year),
			Type:   mediaType,
			Poster: core.TMDB_IMAGE_BASE_URL + r.PosterPath,
			Year:   year,
		})
	}

	return results, nil
}

func (c *Cinebolt) GetMediaID(mediaURL string) (string, error) {
	u, err := url.Parse(mediaURL)
	if err != nil {
		return "", err
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid URL")
	}
	
	id := parts[1]
	mType := parts[0]
	title := u.Query().Get("title")
	year := u.Query().Get("year")

	// Format: tmdbID|season|episode|title|year
	if mType == "movie" {
		return fmt.Sprintf("%s|0|0|%s|%s", id, title, year), nil
	}
	return fmt.Sprintf("%s|%s|%s|%s", id, mType, title, year), nil // Series handle their own season/episode later
}

func (c *Cinebolt) GetSeasons(mediaID string) ([]core.Season, error) {
	parts := strings.Split(mediaID, "|")
	id := parts[0]
	title := parts[2]
	year := parts[3]

	params := url.Values{}
	req, err := c.newTMDBRequest(fmt.Sprintf("tv/%s", id), params)
	if err != nil {
		return nil, err
	}

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var data core.TmdbShowDetails
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	var seasons []core.Season
	for _, s := range data.Seasons {
		if s.SeasonNumber == 0 {
			continue
		}
		// ID: tmdbID|seasonNumber|title|year
		seasons = append(seasons, core.Season{
			ID:   fmt.Sprintf("%s|%d|%s|%s", id, s.SeasonNumber, title, year),
			Name: s.Name,
		})
	}
	return seasons, nil
}

func (c *Cinebolt) GetEpisodes(id string, isSeason bool) ([]core.Episode, error) {
	parts := strings.Split(id, "|")
	tmdbID := parts[0]
	
	title := parts[2]
	year := parts[3]

	if isSeason {
		seasonNum := parts[1]
		params := url.Values{}
		req, err := c.newTMDBRequest(fmt.Sprintf("tv/%s/season/%s", tmdbID, seasonNum), params)
		if err != nil {
			return nil, err
		}

		resp, err := c.Client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		var data core.TmdbSeasonDetails
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			return nil, err
		}

		var episodes []core.Episode
		for _, e := range data.Episodes {
			// ID: tmdbID|seasonNum|episodeNum|title|year
			episodes = append(episodes, core.Episode{
				ID:   fmt.Sprintf("%s|%s|%d|%s|%s", tmdbID, seasonNum, e.EpisodeNumber, title, year),
				Name: fmt.Sprintf("E%02d - %s", e.EpisodeNumber, e.Name),
			})
		}
		return episodes, nil
	} else {
		// Movie: ID: tmdbID|0|0|title|year
		return []core.Episode{{ID: tmdbID + "|0|0|" + title + "|" + year, Name: "Movie"}}, nil
	}
}

func (c *Cinebolt) GetServers(episodeID string) ([]core.Server, error) {
	parts := strings.Split(episodeID, "|")
	tmdbID := parts[0]
	season := parts[1]
	episode := parts[2]
	title := parts[3]
	year := parts[4]

	nonce := uuid.New().String()

	params := url.Values{}
	params.Set("tmdb_id", tmdbID)
	params.Set("nonce", nonce)
	if season != "0" {
		params.Set("season", season)
		params.Set("episode", episode)
	}

	watchURL := fmt.Sprintf("%s/movie/%s", CINEBOLT_BASE_URL, tmdbID)
	if season != "0" {
		watchURL = fmt.Sprintf("%s/tv/%s/watch?play=true&season=%s&episode=%s", CINEBOLT_BASE_URL, tmdbID, season, episode)
	}

	req, err := c.newCineboltRequest("GET", "stream-token", params, watchURL)
	if err != nil {
		return nil, err
	}

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("stream-token API returned status %d", resp.StatusCode)
	}

	var tokenRes struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenRes); err != nil {
		return nil, fmt.Errorf("error decoding token: %w", err)
	}

	// Now call stream API
	params.Set("token", tokenRes.Token)
	params.Set("title", title)
	params.Set("year", year)
	
	req, err = c.newCineboltRequest("GET", "stream/"+tmdbID, params, watchURL)
	if err != nil {
		return nil, err
	}

	resp, err = c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("stream API returned status %d", resp.StatusCode)
	}

	var streamRes struct {
		Sources []struct {
			File   string `json:"file"`
			Label  string `json:"label"`
			Server string `json:"server"`
		} `json:"sources"`
		Subtitles []struct {
			URL   string `json:"url"`
			Lang  string `json:"lang"`
			Label string `json:"label"`
		} `json:"subtitles"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&streamRes); err != nil {
		return nil, fmt.Errorf("error decoding stream: %w", err)
	}

	var servers []core.Server
	for _, s := range streamRes.Sources {
		id := s.File
		var subs []string
		if len(streamRes.Subtitles) > 0 {
			for _, sub := range streamRes.Subtitles {
				label := strings.ToLower(sub.Label)
				if strings.Contains(label, "english") || strings.Contains(label, "eng") || label == "en" {
					subs = append(subs, sub.URL)
				}
			}
		}

		wyzieURL := fmt.Sprintf("https://sub.wyzie.io/search?id=%s&key=wyzie-0a2c83cd296cdb3ac993c4b57f7abf9a", tmdbID)
		if season != "0" {
			wyzieURL += fmt.Sprintf("&season=%s&episode=%s", season, episode)
		}
		if wyzieReq, err := http.NewRequest("GET", wyzieURL, nil); err == nil {
			wyzieReq.Header.Set("User-Agent", CINEBOLT_USER_AGENT)
			if wyzieResp, err := c.Client.Do(wyzieReq); err == nil && wyzieResp.StatusCode == 200 {
				var wyzieData []struct {
					URL      string `json:"url"`
					Language string `json:"language"`
					Display  string `json:"display"`
				}
				if json.NewDecoder(wyzieResp.Body).Decode(&wyzieData) == nil {
					for _, sub := range wyzieData {
						label := strings.ToLower(sub.Display)
						if strings.Contains(label, "english") || strings.Contains(label, "eng") || sub.Language == "en" {
							subs = append(subs, sub.URL)
						}
					}
				}
				wyzieResp.Body.Close()
			}
		}

		if len(subs) > 0 {
			id = fmt.Sprintf("%s|subs=%s", s.File, strings.Join(subs, ","))
		}
		servers = append(servers, core.Server{
			ID:   id,
			Name: fmt.Sprintf("%s (%s)", s.Server, s.Label),
		})
	}

	return servers, nil
}

func (c *Cinebolt) GetLink(serverID string) (string, error) {
	return serverID, nil
}
