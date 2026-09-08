(import spork/path)

(use judge)
(use ../src/xref)

(def fixture-root (path/abspath "test/resources/xref"))

(defn analyze-fixture
  [name]
  (let [source-path (path/join fixture-root name)]
    (analyze (path->uri source-path) (slurp source-path))))

(def analyses
  [(analyze-fixture "alias.janet")
   (analyze-fixture "consumer.janet")
   (analyze-fixture "model.janet")])

(def target-path (path/join fixture-root "model.janet"))

(deftest "cross-module references include aliases and exclude shadowed bindings"
  (let [found (references analyses target-path "target" true)]
    (test (map |(get-in $ [:token :value]) found)
          @["m/target" "model/target" "target" "target"])
    (test (map |(get-in $ [:token :line]) found)
          @[3 3 1 10])))

(deftest "reference lookup can exclude the declaration"
  (test (map |(get-in $ [:token :value])
             (references analyses target-path "target" false))
        @["m/target" "model/target" "target"]))

(deftest "rename preserves module qualifiers"
  (let [found (references analyses target-path "target" true)
        edit (rename-edit found "renamed")]
    (test (map |(get-in $ [:newText])
               (get-in edit [:changes (path->uri (path/join fixture-root "alias.janet"))]))
          @["m/renamed"])
    (test (map |(get-in $ [:newText])
               (get-in edit [:changes (path->uri (path/join fixture-root "consumer.janet"))]))
          @["model/renamed"])
    (test (map |(get-in $ [:newText])
               (get-in edit [:changes (path->uri target-path)]))
          @["renamed" "renamed"])))
