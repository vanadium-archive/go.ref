import { default as es } from "npm:event-stream"

export var streamUtil = {
  split: es.split,
  map: es.map,
  writeArray: es.writeArray
};