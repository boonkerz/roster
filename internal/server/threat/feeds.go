// Package threat gleicht Quell-IPs mit öffentlichen Blocklisten ab (Bedrohungsdaten):
// Feeds werden täglich geladen, in der Datenbank zwischengespeichert und im Speicher als
// Index gehalten. Lookup ist eine Paketfunktion wie geoip.Country, damit der Store beim
// Lesen von Vorfällen, Ereignissen und Sperren anreichern kann.
package threat

// Feed beschreibt eine Blockliste.
type Feed struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	URL     string `json:"url"`
	Comment byte   `json:"-"` // Kommentarzeichen der Liste (';' oder '#')
}

// Feeds sind die unterstützten Listen (alle frei, ohne Schlüssel; Plain-Text).
var Feeds = []Feed{
	{ID: "spamhaus_drop", Name: "Spamhaus DROP", URL: "https://www.spamhaus.org/drop/drop.txt", Comment: ';'},
	{ID: "blocklist_de", Name: "blocklist.de", URL: "https://lists.blocklist.de/lists/all.txt", Comment: '#'},
	{ID: "feodo", Name: "Feodo Tracker", URL: "https://feodotracker.abuse.ch/downloads/ipblocklist.txt", Comment: '#'},
	{ID: "cins", Name: "CINS Army", URL: "https://cinsscore.com/list/ci-badguys.txt", Comment: '#'},
	{ID: "tor_exit", Name: "Tor-Exit-Nodes", URL: "https://check.torproject.org/torbulkexitlist", Comment: '#'},
}

// FeedByID liefert den Feed zu einer ID.
func FeedByID(id string) (Feed, bool) {
	for _, f := range Feeds {
		if f.ID == id {
			return f, true
		}
	}
	return Feed{}, false
}
