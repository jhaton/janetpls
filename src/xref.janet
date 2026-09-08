(import spork/path)

(import ./parser)

(def ignored-directories
  {".git" true
   ".janet-lsp" true
   "build" true
   "jpm_tree" true})

(defn uri->path
  [uri]
  (path/abspath
    (cond
      (string/has-prefix? "file://" uri) (string/slice uri 7)
      (string/has-prefix? "file:" uri) (string/slice uri 5)
      uri)))

(defn path->uri
  [file-path]
  (string "file://" (path/abspath file-path)))

(defn location
  [token]
  (let [line (max 0 (dec (get token :line)))
        start (max 0 (dec (get token :col)))
        end (+ start (get token :len))]
    {:uri (get token :uri)
     :range {:start {:line line :character start}
             :end {:line line :character end}}}))

(defn- collect-leaves!
  [node leaves]
  (when (dictionary? node)
    (let [value (get node :value)]
      (if (string? value)
        (array/push leaves node)
        (when (indexed? value)
          (each child value
            (collect-leaves! child leaves))))))
  leaves)

(defn leaves
  [node]
  (collect-leaves! node @[]))

(defn- tagged-children
  [node tag]
  (filter |(= tag (get $ :tag))
          (filter dictionary? (get node :value @[]))))

(defn- add-binding!
  [bindings token start end]
  (array/push bindings
              {:name (get token :value)
               :start start
               :end end
               :declaration-index (get token :index)}))

(defn- add-parameter-bindings!
  [node end bindings]
  (each parameters (tagged-children node :parameters)
    (each token (leaves parameters)
      (add-binding! bindings token (+ (get token :index) (get token :len)) end))))

(defn- add-let-bindings!
  [node end bindings]
  (let [parameters (first (tagged-children node :parameters))
        expressions (first (tagged-children node :expr))
        parameter-tokens (if parameters (leaves parameters) @[])
        expression-nodes (if expressions (get expressions :value @[]) @[])]
    (for index 0 (length parameter-tokens)
      (let [token (get parameter-tokens index)
            expression (get expression-nodes index)
            start (if (dictionary? expression)
                    (+ (get expression :index) (get expression :len))
                    (+ (get token :index) (get token :len)))]
        (add-binding! bindings token start end)))))

(defn- collect-bindings!
  [node top-level? parent-end bindings]
  (when (dictionary? node)
    (let [tag (get node :tag)
          node-end (+ (get node :index 0) (get node :len 0))]
      (case tag
        :defn (add-parameter-bindings! node node-end bindings)
        :lambda (add-parameter-bindings! node node-end bindings)
        :for-each (add-parameter-bindings! node node-end bindings)
        :loop (add-parameter-bindings! node node-end bindings)
        :let (add-let-bindings! node node-end bindings)
        nil)
      (when (and (not top-level?) (= tag :defn))
        (each function-name (tagged-children node :fn)
          (each token (leaves function-name)
            (add-binding! bindings token
                          (+ (get token :index) (get token :len))
                          parent-end))))
      (when (and (not top-level?) (= tag :def))
        (each variables (tagged-children node :variables)
          (each token (leaves variables)
            (add-binding! bindings token
                          (+ (get token :index) (get token :len))
                          parent-end))))
      (when (indexed? (get node :value))
        (each child (get node :value)
          (collect-bindings! child false node-end bindings)))))
  bindings)

(defn- top-level-definitions
  [tree]
  (let [definitions @{}]
    (each node (get tree :value @[])
      (case (get node :tag)
        :defn (each function-name (tagged-children node :fn)
                (each token (leaves function-name)
                  (put definitions (get token :value) token)))
        :def (each variables (tagged-children node :variables)
               (each token (leaves variables)
                 (put definitions (get token :value) token)))
        nil))
    definitions))

(defn- module-alias
  [module-name]
  (let [base (path/basename module-name)]
    (if (string/has-suffix? ".janet" base)
      (string/slice base 0 -6)
      base)))

(defn- module-path
  [source-path module-name]
  (let [base (path/abspath (path/join (path/dirname source-path) module-name))
        file-candidate (string base ".janet")
        init-candidate (path/join base "init.janet")]
    (cond
      (= :file (os/stat base :mode)) base
      (= :file (os/stat file-candidate :mode)) file-candidate
      (= :file (os/stat init-candidate :mode)) init-candidate
      nil)))

(defn- imports
  [source-path tree]
  (let [found @[]]
    (each node (get tree :value @[])
      (when (= :ptuple (get node :tag))
        (let [tokens (leaves node)]
          (when (and (>= (length tokens) 2)
                     (= "import" (get-in tokens [0 :value])))
            (let [module-name (get-in tokens [1 :value])
                  alias-index (find-index |(= ":as" (get $ :value)) tokens)
                  alias (if alias-index
                          (get-in tokens [(inc alias-index) :value])
                          (module-alias module-name))
                  imported-path (module-path source-path module-name)]
              (when (and imported-path alias)
                (array/push found {:alias alias :path imported-path})))))))
    found))

(defn analyze
  [uri source]
  (let [source-path (uri->path uri)
        canonical-uri (path->uri source-path)
        tree (parser/make-tree source)
        tokens (map |(merge @{} $ {:uri canonical-uri}) (leaves tree))
        raw-definitions (top-level-definitions tree)
        definitions @{}
        bindings @[]]
    (each [name token] (pairs raw-definitions)
      (put definitions name (merge @{} token {:uri canonical-uri})))
    (each node (get tree :value @[])
      (collect-bindings! node true (length source) bindings))
    {:uri canonical-uri
     :path source-path
     :source source
     :tokens tokens
     :definitions definitions
     :imports (imports source-path tree)
     :bindings bindings}))

(defn token-at
  [analysis position]
  (let [line (get position "line" (get position :line))
        character (get position "character" (get position :character))]
    (find |(and (= (dec (get $ :line)) line)
                (<= (dec (get $ :col)) character)
                (<= character (+ (dec (get $ :col)) (get $ :len))))
          (get analysis :tokens))))

(defn- shadowed?
  [analysis token name]
  (some |(and (= name (get $ :name))
              (or (= (get token :index) (get $ :declaration-index))
                  (and (<= (get $ :start) (get token :index))
                       (< (get token :index) (get $ :end)))))
        (get analysis :bindings)))

(defn- qualified-name
  [alias name]
  (if (= alias "") name (string alias "/" name)))

(defn references
  [analyses target-path target-name include-declaration?]
  (let [canonical-target (path/abspath target-path)
        results @[]]
    (each analysis analyses
      (if (= canonical-target (get analysis :path))
        (each token (get analysis :tokens)
          (when (and (= target-name (get token :value))
                     (not (shadowed? analysis token target-name))
                     (or include-declaration?
                         (not= token (get-in analysis [:definitions target-name]))))
            (array/push results {:token token :replacement target-name})))
        (each imported (get analysis :imports)
          (when (= canonical-target (get imported :path))
            (let [reference-name (qualified-name (get imported :alias) target-name)]
              (each token (get analysis :tokens)
                (when (and (= reference-name (get token :value))
                           (not (shadowed? analysis token reference-name)))
                  (array/push results {:token token :replacement reference-name}))))))))
    (sort-by |[(get-in $ [:token :uri]) (get-in $ [:token :index])] results)))

(defn rename-edit
  [references new-name]
  (let [changes @{}]
    (each reference references
      (let [token (get reference :token)
            old-name (get token :value)
            parts (string/split "/" old-name)
            replacement (if (> (length parts) 1)
                          (string/join
                            (array/concat
                              (array/slice parts 0 (dec (length parts)))
                              [new-name])
                            "/")
                          new-name)
            uri (get token :uri)
            edits (get changes uri @[])]
        (array/push edits {:range (get (location token) :range)
                           :newText replacement})
        (put changes uri edits)))
    {:changes changes}))

(defn- find-janet-files!
  [root files]
  (case (os/stat root :mode)
    :directory (unless (get ignored-directories (path/basename root))
                 (each entry (os/dir root)
                   (find-janet-files! (path/join root entry) files)))
    :file (when (string/has-suffix? ".janet" root)
            (array/push files (path/abspath root)))
    nil)
  files)

(defn- git-janet-files
  []
  (try
    (with [process (os/spawn
                     ["git" "ls-files" "--cached" "--others"
                      "--exclude-standard" "--" "*.janet"]
                     :xp {:out :pipe})]
      (let [[output status] (ev/gather
                              (ev/read (process :out) :all)
                              (os/proc-wait process))]
        (when (= status 0)
          (map |(path/abspath $)
               (filter |(> (length $) 0)
                       (string/split "\n" output))))))
    ([error] nil)))

(defn workspace-analyses
  [state]
  (let [contents @{}
        source-paths (or (git-janet-files)
                         (find-janet-files! (os/cwd) @[]))]
    (each source-path source-paths
      (put contents source-path (slurp source-path)))
    (each [uri document] (pairs (get state :documents @{}))
      (put contents (uri->path uri) (get document :content)))
    (seq [[source-path source] :in (pairs contents)]
      (analyze (path->uri source-path) source))))
