package main

import (
	"fmt"
	"github.com/gorilla/websocket"
	jsoniter "github.com/json-iterator/go"
	"math/rand"
	"time"
)

type Anon struct {
	Search bool
	Id     string
	With   string
	PubKey string
	Conn   *websocket.Conn
}

type Msg struct {
	Id      string
	Message string
}

var (
	Anons = make(map[string]Anon)

	json = jsoniter.ConfigCompatibleWithStandardLibrary

	ws = struct {
		Broadcast chan []byte
		Send      chan Msg
		Del       chan Anon
		Add       chan Anon
		Get       chan Anon
		Set       chan Anon
		Exit      chan Anon
		Search    chan Anon
	}{
		Broadcast: make(chan []byte),
		Send:      make(chan Msg),
		Del:       make(chan Anon),
		Add:       make(chan Anon),
		Get:       make(chan Anon),
		Set:       make(chan Anon),
		Exit:      make(chan Anon),
		Search:    make(chan Anon),
	}
)

func anonRead(conn *websocket.Conn) {
	defer conn.Close()

	anon := Anon{}

	defer func() {
		ws.Del <- anon
	}()
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			fmt.Println(anon.Id, err.Error())
			return
		}

		input := struct {
			Id      string `json:"token"`
			PubKey  string `json:"key"`
			Type    string `json:"type"`
			Message string `json:"message"`
		}{}

		if err := json.Unmarshal(message, &input); err != nil {
			fmt.Println("Input json error", err.Error())
			continue
		}

		if len(anon.Id) == 0 {
			if input.Type == "register" && len(input.Id) > 10 && len(input.PubKey) > 16 {
					ws.Add <- Anon{Conn: conn, Id: input.Id, PubKey: input.PubKey, Search: false,}
					anon = <-ws.Add
					continue
				}
			continue
		}

		if input.Type == "system" && input.Message == "exit" {
			ws.Exit <- anon
			continue
		}

		if input.Type == "system" && input.Message == "stop_search" {
			ws.Get <- anon
			anon = <-ws.Get
			if len(anon.With) == 0 {
				anon.Search = false
				ws.Set <- anon
			}
			continue
		}

		if input.Type == "system" && input.Message == "search" {
			ws.Get <- anon
			anon = <-ws.Get
			if len(anon.With) == 0 {
				anon.Search = true
				ws.Set <- anon
				ws.Search <- anon
			}
			continue
		}

		if input.Type == "chat" {
			msg := string(input.Message)
			if len(msg) > 0 {
				ws.Send <- Msg{Id: anon.Id, Message: msg}
			}
			continue
		}
	}
}

func anonChat() {
	type Search struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}

	type Chat struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}

	for {
		ticker := time.NewTicker(30 * time.Second)
		select {
		case client := <-ws.Add:
			if checkMap(client.Id) {
				ws.Add <- client
			}
			for _, anon := range Anons {
				if client.Id == anon.With {
					client.Search = false
					client.With = anon.Id
					if checkMap(anon.Id) {
						j, err := json.Marshal(Search{Type: "system", Message: "reconnect"})
						if err == nil {
							sendWS(Anons[anon.Id].Conn, j, anon.Id)
						}
					}
					break
				}
			}
			Anons[client.Id] = client
			ws.Add <- client
			fmt.Println(Anons)

		case client := <-ws.Del:
			anon := Anons[client.Id].With
			if checkMap(anon) {
				j, err := json.Marshal(Search{Type: "system", Message: "disconnect"})
				if err == nil {
					sendWS(Anons[anon].Conn, j, anon)
				}
			}
			delete(Anons, client.Id)

		case client := <-ws.Get:
			ws.Get <- Anons[client.Id]

		case client := <-ws.Set:
			Anons[client.Id] = client

		case client := <-ws.Search:
			anon, ok := getRandAnon(client.Id)
			if !ok || len(Anons[client.Id].With) != 0 {
				break
			}

			fmt.Println("Search", client.Id, "find", anon)
			Anons[client.Id] = Anon{Conn: Anons[client.Id].Conn, Id: Anons[client.Id].Id, Search: false, With: anon, PubKey: Anons[client.Id].PubKey}
			Anons[anon] = Anon{Conn: Anons[anon].Conn, Id: Anons[anon].Id, Search: false, With: client.Id, PubKey: Anons[anon].PubKey}

			j, err := json.Marshal(Search{Type: "search", Message: Anons[anon].PubKey})
			if err == nil {
				sendWS(Anons[client.Id].Conn, j, client.Id)
			}

			j, err = json.Marshal(Search{Type: "search", Message: client.PubKey})
			if err == nil {
				sendWS(Anons[anon].Conn, j, anon)
			}

		case client := <-ws.Exit:
			anon := Anons[client.Id].With
			Anons[client.Id] = Anon{Conn: Anons[client.Id].Conn, Id: Anons[client.Id].Id, Search: false, PubKey: Anons[client.Id].PubKey}
			if checkMap(anon) {
				Anons[anon] = Anon{Conn: Anons[anon].Conn, Id: Anons[anon].Id, Search: false, PubKey: Anons[anon].PubKey}
				j, err := json.Marshal(Search{Type: "system", Message: "exit"})
				if err == nil {
					sendWS(Anons[anon].Conn, j, anon)
				}
			}

		case client := <-ws.Send:
			anon := Anons[client.Id].With
			if !checkMap(anon) {
				fmt.Println("Cant send", "from", client.Id, "to", anon, "map:", Anons)
				break
			}
			fmt.Println(client.Id, "to", anon, client.Message)
			j, err := json.Marshal(Chat{Type: "chat", Message: client.Message})
			if err == nil {
				sendWS(Anons[anon].Conn, j, anon)
			}

		case broadcast := <-ws.Broadcast:
			for key, anon := range Anons {
				sendWS(anon.Conn, broadcast, key)
			}

		case <-ticker.C:
			j, err := json.Marshal(Chat{Type: "system", Message: "ping"})
			if err == nil {
				for key, anon := range Anons {
					sendWS(anon.Conn, j, key)
				}
			}
		}
	}
}

func sendWS(conn *websocket.Conn, j []byte, key string) {
	if err := conn.WriteMessage(1, j); err != nil {
		conn.Close()
		delete(Anons, key)
	}
}

func checkMap(key string) bool {
	if _, ok := Anons[key]; ok {
		return true
	}
	return false
}

func getRandAnon(id string) (string, bool) {
	var k []string
	for _, anon := range Anons {
		if !anon.Search || anon.Id == id {
			continue
		}
		k = append(k, anon.Id)
	}
	if len(k) == 0 {
		return "", false
	}
	return k[rand.Intn(len(k))], true
}
