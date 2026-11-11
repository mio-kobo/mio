if exists("b:current_syntax") | finish | endif
syn keyword mioKeyword kaku waza teisu kata sentaku koukai yuzuru
syn keyword mioControl moshi sore aida awase baai kaesu nukeru tsuzuku
syn keyword mioImport yobu
syn keyword mioConstant hai iie mu
syn keyword mioType int8 int16 int32 int64 uint8 uint16 uint32 float32 float64 rune bool string unit resource Map
syn match   mioNumber "\<[0-9]\+\(\.[0-9]\+\)\?\>"
syn region  mioString start=+"+ skip=+\\"+ end=+"+ oneline
syn region  mioRune start=+'+ skip=+\\'+ end=+'+ oneline
syn match   mioLineComment "//.*$"
syn region  mioBlockComment start="/\*" end="\*/"
hi def link mioKeyword Keyword
hi def link mioControl Conditional
hi def link mioImport Include
hi def link mioConstant Constant
hi def link mioType Type
hi def link mioNumber Number
hi def link mioString String
hi def link mioRune Character
hi def link mioLineComment Comment
hi def link mioBlockComment Comment
let b:current_syntax = "mio"
