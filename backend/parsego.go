package main
import("fmt";"go/parser";"go/token";"os")
func main(){_,err:=parser.ParseFile(token.NewFileSet(),os.Args[1],nil,parser.AllErrors); if err!=nil{fmt.Println(err);os.Exit(1)};fmt.Println("go parse ok")}
