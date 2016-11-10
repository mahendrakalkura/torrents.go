package main

import "errors"
import "fmt"
import "github.com/lestrrat/go-libxml2"
import "github.com/lestrrat/go-libxml2/xpath"
import "github.com/olekukonko/tablewriter"
import "net/http"
import "os"
import "regexp"
import "sort"
import "strconv"
import "strings"
import "sync"
import "time"

var wait_group sync.WaitGroup

type torrent struct {
	category string
	seeds    int
	title    string
	url      string
}

type by_category_and_seeds []torrent

func (items by_category_and_seeds) Len() int {
	return len(items)
}

func (items by_category_and_seeds) Swap(one, two int) {
	items[one], items[two] = items[two], items[one]
}

func (items by_category_and_seeds) Less(one, two int) bool {
	if items[one].category < items[two].category {
		return true
	}
	if items[one].category > items[two].category {
		return false
	}
	return items[one].seeds < items[two].seeds
}

func consumer(pages int, incoming chan string, outgoing chan []torrent) {
	defer wait_group.Done()

	var torrents []torrent

	for ts := range outgoing {
		torrents = append(torrents, ts...)
		pages = pages - 1
		if pages == 0 {
			close(incoming)
			close(outgoing)
			break
		}
	}

	sort.Sort(by_category_and_seeds(torrents))

	table := tablewriter.NewWriter(os.Stdout)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetAutoFormatHeaders(false)
	table.SetHeader([]string{"Category", "Seeds", "URL"})
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	for _, torrent := range torrents {
		seeds := strconv.Itoa(torrent.seeds)
		table.Append([]string{torrent.category, seeds, torrent.url})
	}
	table.Render()
}

func producer(incoming chan string, outgoing chan []torrent) {
	defer wait_group.Done()

	for url := range incoming {
		torrents, err := get_torrents(url)
		if err != nil {
			incoming <- url
			continue
		}
		outgoing <- torrents
	}
}

func get_torrents(url string) ([]torrent, error) {
	timeout := time.Duration(15 * time.Second)
	client := http.Client{Timeout: timeout}
	response, err := client.Get(url)
	if err != nil {
		return []torrent{}, errors.New("#1")
	}

	defer response.Body.Close()

	document, err := libxml2.ParseHTMLReader(response.Body)
	if err != nil {
		return []torrent{}, errors.New("#2")
	}

	defer document.Free()

	var torrents []torrent

	trs := xpath.NodeList(document.Find(`//table[@id="searchResult"]/tr`))
	for tr := 0; tr < len(trs); tr++ {
		tds := xpath.NodeList(trs[tr].Find(`.//td`))
		if len(tds) != 4 {
			continue
		}

		seeds_string := xpath.String(tds[2].Find(`.//text()`))
		seeds_integer, err := strconv.Atoi(seeds_string)
		if err != nil {
			continue
		}
		if seeds_integer < 100 {
			continue
		}

		category := xpath.String(tds[0].Find(`.//center`))
		regular_expression := regexp.MustCompile(`\s+`)
		category = regular_expression.ReplaceAllString(category, " ")
		category = strings.TrimSpace(category)
		title := xpath.String(tds[1].Find(`.//div/a/text()`))
		url := xpath.String(tds[1].Find(`.//div/a/@href`))
		url = fmt.Sprintf("https://pirateproxy.yt%s", url)
		torrents = append(
			torrents,
			torrent{
				category: category,
				seeds:    seeds_integer,
				title:    title,
				url:      url,
			},
		)
	}

	return torrents, nil
}

func main() {
	var urls []string

	for page := 1; page <= 30; page++ {
		urls = append(
			urls, fmt.Sprintf("https://pirateproxy.yt/recent/%d", page-1),
		)
	}
	urls = append(urls, "https://pirateproxy.yt/top/48h200")
	urls = append(urls, "https://pirateproxy.yt/top/48h500")

	pages := len(urls)
	incoming := make(chan string)
	outgoing := make(chan []torrent)

	wait_group.Add(1)
	go consumer(pages, incoming, outgoing)

	for index := 1; index <= 32; index++ {
		wait_group.Add(1)
		go producer(incoming, outgoing)
	}

	for _, url := range urls {
		incoming <- url
	}

	wait_group.Wait()
}
