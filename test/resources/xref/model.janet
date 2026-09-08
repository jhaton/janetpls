(defn target [value]
  value)

(defn locally-shadowed [target]
  (target 0))

(let [target (fn [value] value)]
  (target 1))

(target 2)
