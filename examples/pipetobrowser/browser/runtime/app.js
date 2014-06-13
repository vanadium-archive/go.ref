import { Logger } from 'libs/logs/logger'

import { registerNavigateHomePageAction,  navigateHomePage } from 'actions/navigate-home-page'
import { registerDisplayErrorAction } from 'actions/display-error'
import { registerAddPipeViewerAction } from 'actions/add-pipe-viewer'
import { registerNavigatePipesPageAction, navigatePipesPage } from 'actions/navigate-pipes-page'

import { SubPageItem } from 'views/page/view'

import { page } from 'runtime/context'

var log = new Logger('app');

export function start() {

  log.debug('start called');

  // Initialize a new page, sets up toolbar and action bar for the app
  initPageView();

  // Register the action handlers for the application
  registerActions();

  // Start by triggering the home action
  navigateHomePage();
}

/*
 * Registers the action handlers for the application.
 * Actions are cohesive pieces of functionality that can be triggered from
 * any other action in a decoupled way by just using the action name.
 *
 * @private
 */
function registerActions() {

  log.debug('registering actions');

  registerNavigateHomePageAction();
  registerDisplayErrorAction();
  registerAddPipeViewerAction();
  registerNavigatePipesPageAction();
}

/*
 * Constructs a new page, sets up toolbar and action bar for the app
 *
 * @private
 */
function initPageView() {

  // Home, Pipes and Help are top level sub-pages
  var homeSubPageItem = new SubPageItem('home');
  homeSubPageItem.name = 'Home';
  homeSubPageItem.icon = 'home';
  homeSubPageItem.onActivate = navigateHomePage;
  page.subPages.push(homeSubPageItem);

  var pipesSubPageItem = new SubPageItem('pipes');
  pipesSubPageItem.name = 'Pipes';
  pipesSubPageItem.icon = 'arrow-forward';
  pipesSubPageItem.onActivate = navigatePipesPage;
  page.subPages.push(pipesSubPageItem);

  var helpSubPageItem = new SubPageItem('help');
  helpSubPageItem.name = 'Help';
  helpSubPageItem.icon = 'help';
  helpSubPageItem.onActivate = function() {
    alert('Not Implemented');
  };
  page.subPages.push(helpSubPageItem);

  document.body.appendChild(page.element);
}