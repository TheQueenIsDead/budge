package main

type Parser interface {
	ParseCSV(path string)
}

type KiwibankParser struct {
}

func (parser KiwibankParser) ParseCSV(path string) {

}
