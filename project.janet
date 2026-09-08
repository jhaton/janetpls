(declare-project
  :name "janet-lsp"
  :description "A Language Server (LSP) for the Janet Programming Language"
  :version "0.1.0"
  :dependencies ["https://github.com/janet-lang/spork.git"
                 "https://github.com/CFiggers/jayson.git"
                 "https://github.com/CFiggers/cmd.git"
                 "https://github.com/CFiggers/judge.git"])

# (def cflags
#   (case (os/which)
#     :windows []
#     ["-s"]))

# (declare-executable
#   :name "janet-lsp"
#   :entry "src/main.janet"
#   :cflags cflags
#   :install true)

(declare-archive
  :name "janet-lsp"
  :entry "/src/main"
  :deps ["src/doc.janet"
         "src/eval.janet"
         "src/logging.janet"
         "src/lookup.janet"
         "src/parser.janet"
         "src/rpc.janet"
         "src/utils.janet"
         "src/xref.janet"])

(declare-binscript
  :main "src/janet-lsp"
  :hardcode-syspath true
  :is-janet true)
