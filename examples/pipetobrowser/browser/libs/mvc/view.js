/*
 * Base class for all views.
 * View is a simple wrapper that represents a DOM element and exposes the public
 * API of that element. Web Components should be used to encapsulate View
 * functionality under a single DOM element and handle event binding/triggering,
 * templating, life-cycle management and attribute exposure and therefore those
 * features are not duplicated here.
 * @param {DOMelement} el DOM element this view wraps.
 * @class
 */
export class View {
	constructor(el) {
		this._el = el;
	}

	get element() {
		return this._el;
	}
}