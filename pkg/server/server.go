/*
 * Iptv-Proxy is a project to proxyfie an m3u file and to proxyfie an Xtream iptv service (client API).
 * Copyright (C) 2020  Pierre-Emmanuel Jacquier
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <https://www.gnu.org/licenses/>.
 */

package server

import (
	"bytes"
	"fmt"
	"log"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/jamesnetherton/m3u"
	uuid "github.com/satori/go.uuid"
	"github.com/xybydy/iptv-proxy/pkg/config"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v2"
)

var defaultProxyfiedM3UPath = filepath.Join(os.TempDir(), uuid.NewV4().String()+".iptv-proxy.m3u")
var endpointAntiColision = strings.Split(uuid.NewV4().String(), "-")[0]

// Mapping structure to help replacing original values over the air
type Mapping []struct {
	// Valid key options: name, tvg-name, tvg-id, tvg-logo, group-title
	Key  string `yaml:"key"`
	Name string `yaml:"name"`
	Want string `yaml:"want"`
}

func (m Mapping) Get(key, name string) string {
	for _, i := range m {
		if compareStrings(i.Key, key) && compareStrings(i.Name, name) {
			return i.Want
		}
	}
	return ""
}

// Config represent the server configuration
type Config struct {
	*config.ProxyConfig

	// M3U service part
	playlist *m3u.Playlist
	// this variable is set only for m3u proxy endpoints
	track *m3u.Track
	// path to the proxyfied m3u file
	proxyfiedM3UPath string

	endpointAntiColision string

	mapping Mapping
}

// NewServer initialize a new server configuration
func NewServer(config *config.ProxyConfig) (*Config, error) {
	var p m3u.Playlist
	var m3uPath string

	switch {
	case config.RemoteURL.String() != "":
		m3uPath = config.RemoteURL.String()
	case config.M3UFileName != "":
		m3uPath = config.M3UFileName
	case config.XtreamBaseURL == "":
		log.Panic("no m3u file or remote url")
	}

	var err error
	p, err = m3u.Parse(m3uPath)
	if err != nil {
		return nil, err
	}

	mapping := new(Mapping)
	if config.MappingPath != "" {
		var err error

		mapping, err = getMapping(config.MappingPath)
		if err != nil {
			return nil, err
		}
	}

	return &Config{
		config,
		&p,
		nil,
		defaultProxyfiedM3UPath,
		endpointAntiColision,
		*mapping,
	}, nil
}

// Serve the iptv-proxy api
func (c *Config) Serve() error {
	if err := c.playlistInitialization(); err != nil {
		return err
	}

	router := gin.Default()
	router.Use(cors.Default())
	group := router.Group("/")
	c.routes(group)

	return router.Run(fmt.Sprintf("%s:%d", c.HostConfig.Hostname, c.HostConfig.Port))
}

func (c *Config) playlistInitialization() error {
	if len(c.playlist.Tracks) == 0 {
		return nil
	}

	f, err := os.Create(c.proxyfiedM3UPath)
	if err != nil {
		return err
	}
	defer f.Close()

	return c.marshallInto(f, false)
}

// MarshallInto a *bufio.Writer a Playlist.
func (c *Config) marshallInto(into *os.File, xtream bool) error {
	filteredTrack := make([]m3u.Track, 0, len(c.playlist.Tracks))

	ret := 0
	into.WriteString("#EXTM3U\n") // nolint: errcheck
	for i, track := range c.playlist.Tracks {
		var buffer bytes.Buffer

		buffer.WriteString("#EXTINF:")                       // nolint: errcheck
		buffer.WriteString(fmt.Sprintf("%d ", track.Length)) // nolint: errcheck

		c.updateValues(&track)

		for i := range track.Tags {
			if i == len(track.Tags)-1 {
				buffer.WriteString(fmt.Sprintf("%s=%q", track.Tags[i].Name, track.Tags[i].Value)) // nolint: errcheck
				continue
			}
			buffer.WriteString(fmt.Sprintf("%s=%q ", track.Tags[i].Name, track.Tags[i].Value)) // nolint: errcheck
		}

		uri, err := c.replaceURL(track.URI, i-ret, xtream)
		if err != nil {
			ret++
			log.Printf("ERROR: track: %s: %s", track.Name, err)
			continue
		}

		into.WriteString(fmt.Sprintf("%s, %s\n%s\n", buffer.String(), track.Name, uri)) // nolint: errcheck

		filteredTrack = append(filteredTrack, track)
	}
	c.playlist.Tracks = filteredTrack

	return into.Sync()
}

func (c *Config) updateValues(track *m3u.Track) {

	// updating existing tags
	for i := range track.Tags {
		newValue := c.mapping.Get(track.Tags[i].Name, track.Name)
		if newValue != "" {
			track.Tags[i].Value = newValue
		}
	}

	// adding missing tags, searching by old name
	for _, s := range []string{"tvg-name", "tvg-id", "tvg-logo", "group-title"} {
		newValue := c.mapping.Get(s, track.Name)

		if newValue != "" {
			track.Tags = append(track.Tags, m3u.Tag{
				Name:  "tvg-id",
				Value: newValue,
			})
		}
	}

	// updating the name
	newName := c.mapping.Get("name", track.Name)
	if newName != "" {
		track.Name = newName
	}

}

// ReplaceURL replace original playlist url by proxy url
func (c *Config) replaceURL(uri string, trackIndex int, xtream bool) (string, error) {
	if c.KeepOriginalURLs {
		return uri, nil
	}

	oriURL, err := url.Parse(uri)
	if err != nil {
		return "", err
	}

	protocol := "http"
	if c.HTTPS {
		protocol = "https"
	}

	customEnd := strings.Trim(c.CustomEndpoint, "/")
	if customEnd != "" {
		customEnd = fmt.Sprintf("/%s", customEnd)
	}

	uriPath := oriURL.EscapedPath()
	if xtream {
		uriPath = strings.ReplaceAll(uriPath, c.XtreamUser.PathEscape(), c.User.PathEscape())
		uriPath = strings.ReplaceAll(uriPath, c.XtreamPassword.PathEscape(), c.Password.PathEscape())
	} else {
		uriPath = path.Join("/", c.endpointAntiColision, c.User.PathEscape(), c.Password.PathEscape(), fmt.Sprintf("%d", trackIndex), path.Base(uriPath))
	}

	basicAuth := oriURL.User.String()
	if basicAuth != "" {
		basicAuth += "@"
	}

	var newURI string
	if c.CustomStreamHost == "" {
		newURI = fmt.Sprintf(
			"%s://%s%s:%d%s%s",
			protocol,
			basicAuth,
			c.HostConfig.Hostname,
			c.AdvertisedPort,
			customEnd,
			uriPath,
		)
	} else {
		newURI = fmt.Sprintf(
			"%s://%s%s:%d%s%s",
			protocol,
			basicAuth,
			c.CustomStreamHost,
			c.AdvertisedPort,
			customEnd,
			uriPath,
		)
	}

	newURL, err := url.Parse(newURI)
	if err != nil {
		return "", err
	}

	return newURL.String(), nil
}

func getMapping(fpath string) (*Mapping, error) {
	if _, err := os.Stat(fpath); !os.IsNotExist(err) {
		b, err := os.ReadFile(fpath)
		if err != nil {
			return nil, err
		}

		mapping := new(Mapping)
		err = yaml.Unmarshal(b, &mapping)
		if err != nil {
			log.Print(err)
		}
		return mapping, nil
	}
	return nil, os.ErrNotExist
}

func compareStrings(s1, s2 string) bool {
	return strings.TrimSpace(strings.ToLower(s1)) == strings.TrimSpace(strings.ToLower(s2))
}
