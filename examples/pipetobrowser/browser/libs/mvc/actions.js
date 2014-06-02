/*
 * Actions are a similar concept to routes and simply provide an indirection
 * through string names to register and call functions.
 *
 * Using actions to group and call larger, self-contained functionality like
 * page transitions allows the framework to provide undo/back support by using
 * the history API or localStorage.
 *
 * Action handlers that are registered for an action, normally are controllers
 * that glue application services and state with views and handle events.
 * @overview
 */

var registeredHandlers = {};

/*
 * Registers an action and makes it available to be called using only a name.
 * @param {string} name Action's identifier
 * @param {function} handler Callback handler to register for the action.
 * handler will be called with the arguments pass from the caller when action
 * is triggered.
 */
export function register(name, handler) {
  registeredHandlers[name] = handler;
}

/*
 * Calls an action's registered handler.
 * @param {string} name Action's identifier
 * @param {*} [...] args Arguments to be passed to the registered handler.
 * @return {*} Any result returned from the registered handler
 */
export function trigger(name, ...args) {
  var handler = registeredHandlers[name];
  if(!handler) {
    throw new Error('No handler registered for action: ' + name);
  }
  return handler(...args);
}