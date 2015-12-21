package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"sync"
	"time"
)

const (
	hackernewsURL  = "https://hacker-news.firebaseio.com/v0/newstories.json"
	itemURL        = "https://hacker-news.firebaseio.com/v0/item/%d.json"
	hnPollInterval = 5
)

type hnItem struct {
	By          string `json:"by"`
	Descendants int    `json:"descendants"`
	Id          int    `json:"id"`
	Kids        []int  `json:"kids"`
	Time        int    `json:"time"`
	Score       int    `json:"score"`
	Title       string `json:"title"`
	Type        string `json:"type"`
	Url         string `json:"url"`
}

type HNPoller struct {
	// so we can easily see if an item has already been pulld
	items        map[int]*hnItem
	pollInterval time.Duration
	mutex        *sync.RWMutex

	// make this concurrent because it's so slow
	itemChan chan *hnItem

	// for testing purposes limit how many items it will fetch
	testLimit int
}

func NewHNPoller() *HNPoller {
	return &HNPoller{
		// lets use this as default for now
		items:        make(map[int]*hnItem),
		pollInterval: hnPollInterval,
		mutex:        &sync.RWMutex{},
		itemChan:     make(chan *hnItem, 500),

		testLimit: 20,
	}
}

func (p *HNPoller) poll() {
	for {
		if err := p.fetch(); err != nil {
			fmt.Println(err)
		}

		log.Println("hnpoller: successful pull")
		time.Sleep(p.pollInterval * time.Second)
	}
}

func (p *HNPoller) Items() map[int]*hnItem {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	return p.items
}

func (p *HNPoller) fetch() error {

	// response is a list of ids which we'll later query each and everyone
	body, _ := doRequest(hackernewsURL)

	var ids []int
	if err := json.Unmarshal(body, &ids); err != nil {
		return errors.New("hnpoller: error unmarshalling json")
	}

	if p.testLimit > 0 {
		ids = ids[:p.testLimit]
	}

	// we will give benefit of the doubt and assume that ids are sequential.
	// there will be no erratic bs that will mess up looking for dups
	var newIds []int
	p.mutex.RLock()
	for i, id := range ids {
		if _, ok := p.items[id]; !ok {
			newIds = ids[i:]
			break
		}
	}
	p.mutex.RUnlock()
	log.Printf("hnpoller: %d new ids\n", len(newIds))

	for i, id := range newIds {

		go func(instance int, id int) {
			itemResp, _ := doRequest(fmt.Sprintf(itemURL, id))

			var resp hnItem
			if err := json.Unmarshal(itemResp, &resp); err != nil {
				log.Println("hnpoller: error unmarshaling json")
			}

			fmt.Printf("%d: item %d got: %d\n", instance, resp.Id, resp.Score)
			p.itemChan <- &resp
		}(i, id)
	}

	return nil
}

func (p *HNPoller) itemListener() {
	for {
		item := <-p.itemChan

		p.mutex.Lock()
		p.items[item.Id] = item
		p.mutex.Unlock()
	}
}

func (p *HNPoller) start() {
	go p.poll()
	go p.itemListener()
}

func doRequest(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("error retreiving URL %s\n", url))
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.New("error reading response body")
	}

	return body, nil
}

var poller *HNPoller

func homeHandler(w http.ResponseWriter, r *http.Request) {
	things := poller.Items()

	t, _ := template.ParseFiles("templates/index.html")
	t.Execute(w, map[string]interface{}{
		"items": things,
	})
}

func main() {
	poller = NewHNPoller()
	poller.start()

	http.HandleFunc("/foo", homeHandler)
	log.Fatal(http.ListenAndServe(":8080", nil))
}
