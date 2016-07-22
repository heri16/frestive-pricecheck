package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/tarm/serial"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type Item struct {
	Upc       string `json:"upc"`
	Name      string `json:"name"`
	NameShort string `json:"nameShort"`
	Price     int    `json:"price"`
	Unit      string `json:"unit"`
	UseTax    bool   `json:"useTax"`
}

type ItemResponse struct {
	Data       *Item `json:"data"`
	StatusCode int
}

var escPosVfdCommands = map[string]string{
	"setHorizontalScrollMode":     "\x1F\x03",
	"moveCursorRight":             "\x09",
	"moveCursorLeft":              "\x08",
	"moveCursorUp":                "\x1F\x0A",
	"moveCursorDown":              "\x0A",
	"moveCursorRightMostPosition": "\x1F\x0D",
	"moveCursorLeftMostPosition":  "\x0D",
	"moveCursorHomePosition":      "\x0B",
	"moveCursorBottomPosition":    "\x1F\x42",
	"cursorGoto":                  "\x1F\x24", // 1F 24 x y (1 <= x <= 20; 1 <= y <= 2)
	"cursorDisplay":               "\x1f\x43", // 1F 43 n (n=0, hide; n=1, show)
	"clearScreen":                 "\x0C",
	"clearCursorLine":             "\x18",
	"brightness":                  "\x1F\x58", // 1F 58 n (1 <= n <= 4)
	"blinkDisplay":                "\x1F\x45", // 1F 45 n (0 < n < 255 (n*50msec ON / n*50msec OFF; n=0, blink canceled; n=255, display turned off)
}

func main() {
	port0 := os.Getenv("PORT_BARCODE_SCANNER")
	port1 := os.Getenv("PORT_POLE_DISPLAY")

	if port0 == "" || port1 == "" {
		port0 = "/dev/ttyACM0"
		port1 = "/dev/ttyACM1"
	}

	// Serial Port for Barcode Scanner
	sConf0 := &serial.Config{Name: port0, Baud: 9600}
	sPort0, err := serial.OpenPort(sConf0)
	if err != nil {
		log.Fatal(err)
		// os.Exit(1)
	}
	defer sPort0.Close()

	// Serial Port for VFD Pole Display
	sConf1 := &serial.Config{Name: port1, Baud: 9600}
	sPort1, err := serial.OpenPort(sConf1)
	if err != nil {
		log.Fatal(err)
		// os.Exit(1)
	}
	defer sPort1.Close()

	// Timer to RESET VFD Pole Display after timeout
	displayResetTimer := time.AfterFunc(10*time.Minute, func() {
		sPort1.Write([]byte(escPosVfdCommands["clearScreen"] + escPosVfdCommands["moveCursorHomePosition"] + escPosVfdCommands["clearCursorLine"] + "- CEK HARGA DISINI -" + escPosVfdCommands["moveCursorBottomPosition"] + escPosVfdCommands["clearCursorLine"] + "   Praktis & Cepat  "))
	})

	// Set up a done channel that's shared by the whole pipeline,
	// and close that channel when this pipeline exits, as a signal
	// for all the goroutines we started to exit.
	done := make(chan struct{})
	defer close(done)

	// Set up the pipeline.
	c0 := readScanner(done, sPort0, displayResetTimer)
	c1 := displayBarcode(done, c0, sPort1)
	c2 := queryScannedItem(done, c1)
	out := displayItemResponse(done, c2, sPort1, displayResetTimer)

  displayResetTimer.Reset(1 * time.Second)
	time.AfterFunc(2*time.Second, func() {
		displayResetTimer.Reset(30 * time.Second)
	})
	fmt.Println("Ready.")

	// Consume the output.
	for output := range out {
		fmt.Printf("%#v\n", output)
	}

	// done will be closed by the deferred call.
}

func readScanner(done <-chan struct{}, serialPort *serial.Port, displayResetTimer *time.Timer) <-chan string {
	out := make(chan string)

	go func() {
		defer close(out)

		scanner := bufio.NewScanner(serialPort)
		scanner.Split(ScanCRLines)
		for scanner.Scan() {
			displayResetTimer.Stop()

			select {
			case out <- scanner.Text():
			case <-done:
				return
			}
		}

		if err := scanner.Err(); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
		}
	}()

	return out
}

func displayBarcode(done <-chan struct{}, in <-chan string, serialPort *serial.Port) <-chan string {
	out := make(chan string)

	go func() {
		defer close(out)

		for barcode := range in {
			if _, err := serialPort.Write([]byte(escPosVfdCommands["moveCursorHomePosition"] + escPosVfdCommands["clearCursorLine"])); err != nil {
				log.Panic(err)
			}
			if _, err := serialPort.Write([]byte("Checking Item...")); err != nil {
				log.Panic(err)
			}
			if _, err := serialPort.Write([]byte(escPosVfdCommands["moveCursorBottomPosition"] + escPosVfdCommands["clearCursorLine"])); err != nil {
				log.Panic(err)
			}
			if _, err := serialPort.Write([]byte(barcode)); err != nil {
				log.Panic(err)
			}
			fmt.Println("Scanned: " + barcode)

			select {
			case out <- barcode:
			case <-done:
				return
			}
		}
	}()

	return out
}

func queryScannedItem(done <-chan struct{}, in <-chan string) <-chan *ItemResponse {
	out := make(chan *ItemResponse)

	go func() {
		defer close(out)

		client := &http.Client{}

		for barcode := range in {
			itemNo := BarcodeToItemNo(barcode)

			req, err := http.NewRequest("GET", "http://pos1.cps:4000/api/v1/items/"+itemNo, nil)
			req.Header.Add("Accept", "application/json")
			resp, err := client.Do(req)
			if err != nil {
				log.Println(err)
				out <- &ItemResponse{StatusCode: -1}
				continue
			}

			itemResponse := ItemResponse{StatusCode: resp.StatusCode}

			switch resp.StatusCode {
			case http.StatusOK:
				err = json.NewDecoder(resp.Body).Decode(&itemResponse)
				if err != nil {
					log.Println(err)
					continue
				}

			case http.StatusNotFound, http.StatusInternalServerError:
				fmt.Println("Not found for Item: " + itemNo)

			default:
				contents, err := ioutil.ReadAll(resp.Body)
				if err != nil {
					log.Println(err)
					continue
				}
				fmt.Println("Server error for Item: " + itemNo)
				fmt.Printf("%s\n", string(contents))
			}

			resp.Body.Close()
			select {
			case out <- &itemResponse:
			case <-done:
				return
			}

		}
	}()

	return out
}

func displayItemResponse(done <-chan struct{}, in <-chan *ItemResponse, serialPort *serial.Port, displayResetTimer *time.Timer) <-chan *ItemResponse {
	out := make(chan *ItemResponse)

	go func() {
		defer close(out)

		for itemResponse := range in {
			switch itemResponse.StatusCode {
			case 200:
				switch itemResponse.Data {
				case nil:
					if _, err := serialPort.Write([]byte(escPosVfdCommands["moveCursorHomePosition"] + escPosVfdCommands["clearCursorLine"])); err != nil {
						log.Panic(err)
					}
					if _, err := serialPort.Write([]byte("Item Invalid")); err != nil {
						log.Panic(err)
					}
					if _, err := serialPort.Write([]byte(escPosVfdCommands["moveCursorBottomPosition"] + escPosVfdCommands["clearCursorLine"])); err != nil {
						log.Panic(err)
					}
					if _, err := serialPort.Write([]byte("Mohon cek di kasir")); err != nil {
						log.Panic(err)
					}
				default:
					if _, err := serialPort.Write([]byte(escPosVfdCommands["moveCursorHomePosition"] + escPosVfdCommands["clearCursorLine"])); err != nil {
						log.Panic(err)
					}
					if _, err := serialPort.Write([]byte(itemResponse.Data.NameShort)); err != nil {
						log.Panic(err)
					}
					if _, err := serialPort.Write([]byte(escPosVfdCommands["moveCursorBottomPosition"] + escPosVfdCommands["clearCursorLine"])); err != nil {
						log.Panic(err)
					}
					if _, err := serialPort.Write([]byte(fmt.Sprintf("Rp. %-12d/%3s", itemResponse.Data.Price, itemResponse.Data.Unit))); err != nil {
						log.Panic(err)
					}
				}

			default:
				if _, err := serialPort.Write([]byte(escPosVfdCommands["moveCursorHomePosition"] + escPosVfdCommands["clearCursorLine"])); err != nil {
					log.Panic(err)
				}
				if _, err := serialPort.Write([]byte("Server Bermasalah")); err != nil {
					log.Panic(err)
				}
				if _, err := serialPort.Write([]byte(escPosVfdCommands["moveCursorBottomPosition"] + escPosVfdCommands["clearCursorLine"])); err != nil {
					log.Panic(err)
				}
				if _, err := serialPort.Write([]byte("Mohon cek di kasir")); err != nil {
					log.Panic(err)
				}

			}

			displayResetTimer.Reset(15 * time.Second)

			select {
			case out <- itemResponse:
			case <-done:
				return
			}
		}
	}()

	return out
}

func BarcodeToItemNo(barcode string) string {
	if strings.HasPrefix(barcode, "24") {
		if len(barcode) >= 7 {
			return barcode[0:7]
		}
	}
	return barcode
}

func ScanCRLines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexByte(data, '\r'); i >= 0 {
		// We have a full newline-terminated line.
		return i + 1, data[0:i], nil
	}
	// If we're at EOF, we have a final, non-terminated line. Return it.
	if atEOF {
		return len(data), data, nil
	}
	// Request more data.
	return 0, nil, nil
}
