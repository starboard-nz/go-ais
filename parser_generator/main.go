package main

import (
	"flag"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"log"
	"os"
	"path"
	"strconv"
	"strings"
	"unicode"
)

var inputFile = flag.String("input", "", "Input file")
var outputDir = flag.String("output", "", "Output directory")

var msgMap = map[int]string{
	1:  "PositionReport",
	2:  "PositionReport",
	3:  "PositionReport",
	4:  "BaseStationReport",
	5:  "ShipStaticData",
	6:  "AddressedBinaryMessage",
	7:  "BinaryAcknowledge",
	8:  "BinaryBroadcastMessage",
	9:  "StandardSearchAndRescueAircraftReport",
	10: "CoordinatedUTCInquiry",
	11: "BaseStationReport",
	12: "AddessedSafetyMessage",
	13: "BinaryAcknowledge",
	14: "SafetyBroadcastMessage",
	15: "Interrogation",
	16: "AssignedModeCommand",
	17: "GnssBroadcastBinaryMessage",
	18: "StandardClassBPositionReport",
	19: "ExtendedClassBPositionReport",
	20: "DataLinkManagementMessage",
	21: "AidsToNavigationReport",
	22: "ChannelManagement",
	23: "GroupAssignmentCommand",
	24: "StaticDataReport",
	25: "SingleSlotBinaryMessage",
	26: "MultiSlotBinaryMessage",
	27: "LongRangeAisBroadcastMessage",
}
var numberTypes = map[string]struct{}{
	"ChannelManagementUnicastData":   struct{}{},
	"ChannelManagementBroadcastData": struct{}{},
	"InterrogationStation2":          struct{}{},
	"InterrogationStation1Message1":  struct{}{},
	"InterrogationStation1Message2":  struct{}{},
	"StaticDataReportA":              struct{}{},
	"StaticDataReportB":              struct{}{},
	"FieldApplicationIdentifier":     struct{}{},
	"FieldETA":                       struct{}{},
	"FieldDimension":                 struct{}{},
	"FieldLatLonFine":                struct{}{},
	"FieldLatLonCoarse":              struct{}{},
	"Field10":                        struct{}{},
	"bool":                           struct{}{},
	"int16":                          struct{}{},
	"uint16":                         struct{}{},
	"uint32":                         struct{}{},
	"uint8":                          struct{}{},
}

type fieldType struct {
	name             string
	typ              string
	width            int
	isSkippable      bool
	dependsBit       int
	dependsAs0       bool
	embedded         bool
	isArray          bool
	arrayLength      int
	isVariableLength bool
}

func main() {
	flag.Parse()
	log.Println("Working on", *inputFile)
	if *outputDir == "" {
		var err error
		*outputDir, err = os.Getwd()
		if err != nil {
			panic(err)
		}
	}
	log.Println("output files go in", *outputDir)

	//contents, err := ioutil.ReadFile(*inputFile)
	//if err != nil {
	//	panic(err)
	//}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, *inputFile, nil, parser.ParseComments)
	if err != nil {
		panic(err)
	}
	output := `
// Package ais WARNING: This file is generated by parser_generator/main.go do not edit directly.
package ais

`
	// type to parseFunction
	output += `
var mapper = map[int64]func(t *Codec, payload []byte, offset *uint) Packet {
`
	for i := 1; i < 28; i++ {
		if name, ok := msgMap[i]; ok {
			output += `  ` + strconv.Itoa(i) + `: parse` + name + `,
`
		}
	}
	output += `}
`
	for _, decl := range f.Decls {
		// if it is a struct
		st, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		if st.Tok == token.TYPE {
			for _, spec := range st.Specs {
				typespec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				structType, ok := typespec.Type.(*ast.StructType)
				if !ok {
					continue
				}
				name := typespec.Name.Name
				if !unicode.IsUpper(rune(name[0])) {
					continue
				}

				log.Println("Found type to generate: ", name)

				// the overall minimum length of the message
				var minLength uint

				fields := []fieldType{}
				for _, field := range structType.Fields.List {
					if len(field.Names) == 0 {
						width := 0
						parts := strings.Split(field.Tag.Value, ":")
						if len(parts) == 2 {
							parts[0] = strings.Trim(parts[0], "`\"")
							parts[1] = strings.Trim(parts[1], "`\"")
							if parts[0] == "aisWidth" {
								ml, err := strconv.Atoi(parts[1])
								if err != nil {
									panic(err)
								}
								minLength += uint(ml)
								width = ml
							}
						}
						// it's probably the header embed or communicationStateNoItdma
						fields = append(fields, fieldType{
							embedded: true,
							typ:      field.Type.(*ast.Ident).Name,
							width:    width,
						})
						continue
					}
					fieldName := field.Names[0].Name
					var (
						typ         string
						isArray     bool
						arrayLength int
						width       int
						skippable   bool
						fixedLength bool
						dependsAs0  bool
					)

					dependsBit := -1
					switch msg := field.Type.(type) {
					case *ast.Ident:
						typ = msg.Name
					case *ast.ArrayType:
						var length string
						isArray = true
						if msg.Len != nil && msg.Len.(*ast.BasicLit).Kind == token.INT {
							length = msg.Len.(*ast.BasicLit).Value
							arrayLength, err = strconv.Atoi(length)
							if err != nil {
								panic(err)
							}
						}
						typ = msg.Elt.(*ast.Ident).Name
					}
					if field.Tag == nil {
						fields = append(fields, fieldType{
							name: fieldName,
						})
						continue
					}
					tags := strings.Split(field.Tag.Value, " ")

					for _, tag := range tags {
						parts := strings.Split(tag, ":")
						if len(parts) > 2 {
							panic("unepxected multiple parts to tag")
						}

						tagName := strings.Trim(parts[0], "`")
						tagValue := strings.Trim(parts[1], "`\"")
						//I.E aiswidth:6
						if tagName == "aisWidth" {
							width, err = strconv.Atoi(tagValue)
							if err != nil {
								panic(err)
							}
							if width < 0 {
								fixedLength = true
							} else {
								minLength += uint(width)
							}
						} else if tagName == "aisDependsBit" {
							if tagValue[0] == '~' {
								dependsAs0 = true
								tagValue = tagValue[1:]
							}

							ml, err := strconv.Atoi(tagValue)
							if err != nil {
								panic(err)
							}
							dependsBit = ml
						} else if tagName == "aisOptional" {
							skippable = true
						} else if tagName == "aisEncodeMaxLen" {
							// this branch intentionally left blank
							// does nothing for decoding
						}
					}
					// drop the minlength width if this is an optional field..
					if dependsBit != -1 {
						minLength -= uint(width)
					}
					log.Println("\t", fieldName, typ, tags, width)
					fields = append(fields, fieldType{
						name:             fieldName,
						typ:              typ,
						width:            width,
						arrayLength:      arrayLength,
						isArray:          isArray,
						isSkippable:      skippable,
						isVariableLength: fixedLength,
						dependsBit:       dependsBit,
						dependsAs0:       dependsAs0,
					})
				}

				isPacketType := false
				for _, positionName := range msgMap {
					if name == positionName {
						isPacketType = true
						break
					}
				}
				rv := "p"
				if isPacketType {
					rv = "nil"
					output += `func parse` + name + `(t *Codec, payload []byte, offset *uint) Packet {
`
				} else {
					output += `func parse` + name + `(t *Codec, payload []byte, offset *uint) ` + name + ` {`
				}
				output += `
	p := ` + name + `{}
	minLength := uint(` + strconv.Itoa(int(minLength)) + `)
    minBitsForValid, ok := t.minValidMap["` + name + `"]
	if !ok {
		minBitsForValid = minLength
	}
	if len(payload)-int(*offset) < int(minBitsForValid) {
		return ` + rv + `
	}
var length uint
    `

				var (
					hasNumberParseable bool
					hasStringParseable bool
				)

				for _, field := range fields {
					if field.typ == "string" {
						hasStringParseable = true
					}
					if _, ok := numberTypes[field.typ]; ok {
						hasNumberParseable = true
					}
				}

				if hasNumberParseable {
					output += `
	var num int64
`
				}
				if hasStringParseable {
					output += `
	var str string
`
				}

				for fieldI, field := range fields {
					if field.name == "Valid" {
						output += `
	p.` + field.name + ` = true
`
						continue
					}
					if field.embedded {
						output += `
	p.` + field.typ + ` = parse` + field.typ + `(t, payload, offset)
`

					} else if field.isArray {
						output += `
	// ` + field.name + ` is an array of ` + field.typ + `s
`
						switch field.typ {
						case "AssignedModeCommandData":
							fallthrough
						case "BinaryAcknowledgeData":
							fallthrough
						case "DataLinkManagementMessageData":
							output += `var elems [` + strconv.Itoa(field.arrayLength) + `]` + field.typ + `
`
							for i := 0; i < field.arrayLength; i++ {
								output += `elems[` + strconv.Itoa(i) + `] = parse` + field.typ + `(t, payload, offset)
`
								if field.width > 0 || i == 0 {
									output += `
	if !elems[` + strconv.Itoa(i) + `].Valid {
		return nil
	}
`
								}
							}
							output += `p.` + field.name + ` = elems`
						case "byte":

							if field.width < 0 {
								// if this is the last field we can just take the rest of the payload
								if fieldI == len(fields)-1 {
									output += `length = uint(len(payload)) - *offset
`
								} else {
									remainingWidth := 0
									for i := fieldI + 1; i < len(fields); i++ {
										remainingWidth += fields[i].width
									}
									output += `length = uint(len(payload))-*offset - ` + strconv.Itoa(remainingWidth) + `
`
								}
							} else {
								output += `length = ` + strconv.Itoa(field.width) + `
`
							}

							output += `
	if int(length) < 0 {
		return nil
	}
`

							output += `p.` + field.name + ` = payload[*offset:*offset+length]
	*offset += length
`
						}
					} else {
						output += "\n// parsing " + field.name + " as " + field.typ
						if field.dependsBit > 0 {
							var dependValue = "1"
							if field.dependsAs0 {
								dependValue = "0"
							}
							output += `(optional)
	if len(payload) <= ` + strconv.Itoa(field.dependsBit) + ` {
		// todo set Valid=false??
		return nil
	}
	if payload[` + strconv.Itoa(field.dependsBit) + `] == ` + dependValue + ` {
`

						}
						if field.isVariableLength {
							output += `
	// TODO check minlength less than payload length... TODO check if there is a better way to calculate the variable length...
	length = uint(len(payload)) - minLength` + "\n"
						} else {
							output += `
length = ` + strconv.Itoa(field.width) + "\n"
						}
						// simple type parsing
						switch field.typ {
						case "InterrogationStation2":
							fallthrough
						case "InterrogationStation1Message1":
							fallthrough
						case "InterrogationStation1Message2":
							fallthrough
						case "StaticDataReportA":
							fallthrough
						case "StaticDataReportB":
							fallthrough
						case "FieldApplicationIdentifier":
							output += `p.` + field.name + ` = parse` + field.typ + `(t, payload, offset)
`
							if field.width > 0 {
								output += `
	if !p.` + field.name + `.Valid {
		return nil
	}
`
							}
						case "ChannelManagementUnicastData":
							fallthrough
						case "ChannelManagementBroadcastData":
							fallthrough
						case "FieldETA":
							fallthrough
						case "FieldDimension":
							output += `p.` + field.name + ` = parse` + field.typ + `(t, payload, offset)
`
						case "FieldLatLonFine":
							output += `
	num = extractNumber(payload, true, offset, length)
	if !t.FloatWithoutConversion {
		p.` + field.name + ` = FieldLatLonFine(num) / 10000 / 60
	} else {
		p.` + field.name + ` = FieldLatLonFine(num)
	}
`
						case "FieldLatLonCoarse":
							output += `
	num = extractNumber(payload, true, offset, length)
	if !t.FloatWithoutConversion {
		p.` + field.name + ` = FieldLatLonCoarse(num) / 10 / 60
	} else {
		p.` + field.name + ` = FieldLatLonCoarse(num)
	}
`
						case "Field10":
							output += `
	num = extractNumber(payload, false, offset, length)
	if !t.FloatWithoutConversion {
		p.` + field.name + ` = Field10(num) / 10
	} else {
		p.` + field.name + ` = Field10(num)
	}
`
						case "bool":
							output += `
	num = extractNumber(payload, false, offset, length)
	p.` + field.name + ` = num == 1`
						case "int16":
							output += `
	num = extractNumber(payload, true, offset, length)
	p.` + field.name + ` = int16(num)
`
						case "string":
							output += `str = extractString(payload, offset, length, t.DropSpace)
	p.` + field.name + ` = str
`
						case "uint16":
							output += `
	num = extractNumber(payload, false, offset, length)
	p.` + field.name + ` = uint16(num)
`
						case "uint32":
							output += `
	num = extractNumber(payload, false, offset, length)
	p.` + field.name + ` = uint32(num)
`
						case "uint8":
							output += `
	num = extractNumber(payload, false, offset, length)
	p.` + field.name + ` = uint8(num)
`
						default:
							panic("unhandled type: " + field.typ)
						}

						// check again if we are in an optional type, then close the surrounding condition if so
						if field.dependsBit > 0 {
							output += `
}
`
						}
					}

				}
				if isPacketType {
					output += `
	if *offset > uint(len(payload)) {
		return nil
	}
`
				}
				output += `
	return p
}

`
			}
		}
	}

	// now output back to a file
	outputPath := path.Join(*outputDir, "codec_gen.go")
	formattedContent, err := format.Source([]byte(output))
	if err != nil {
		panic(err)
	}
	err = os.WriteFile(outputPath, formattedContent, 0644)
	if err != nil {
		panic(err)
	}
	log.Println("generated: ", outputPath)
}
