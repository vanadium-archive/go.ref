import { exists } from 'libs/utils/exists'
import { View } from 'libs/mvc/view'
import { Logger } from 'libs/logs/logger'

var log = new Logger('views/error');

/*
 * View representing application error.
 * @param {Error|String} err Error to display
 * @class
 * @extends {View}
 */
export class ErrorView extends View {
	constructor(err) {
		var el = document.createElement('p2b-error');
		super(el);

		this.error = err;
	}

	set error(err) {
		if(!exists(err)) {
			return;
		}

		var errorMessage = err.toString();
		log.debug(errorMessage);
		if(exists(err.stack)) {
			log.debug(err.stack);
		}

		this.element.errorMessage = errorMessage;
	}

	get error() {
		return this.element.errorMessage;
	}
}