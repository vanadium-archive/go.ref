package com.boxes;

import android.annotation.SuppressLint;
import android.annotation.TargetApi;
import android.os.Build;
import android.os.Bundle;
import android.os.Handler;
import android.support.v4.app.Fragment;
import android.view.LayoutInflater;
import android.view.Menu;
import android.view.MenuInflater;
import android.view.MenuItem;
import android.view.View;
import android.view.ViewGroup;

public class DragAndDrawActivity extends SingleFragmentActivity {
	
	private class DragAndDrawFragment extends Fragment {
		private GoRemoteService mGoRemoteService;

		@Override
		public void onCreate(Bundle savedInstanceState) {
			super.onCreate(savedInstanceState);
			setHasOptionsMenu(true);
			mGoRemoteService = new GoRemoteService();
			mGoRemoteService.start();
			mGoRemoteService.getLooper();
		}
		
		@Override
		public View onCreateView(LayoutInflater inflater, ViewGroup parent, Bundle savedInstanceState) {
			return inflater.inflate(R.layout.fragment_drag_and_draw, parent, false);
		}
		
		@Override
		public void onCreateOptionsMenu(Menu menu, MenuInflater inflater) {
			super.onCreateOptionsMenu(menu, inflater);
			inflater.inflate(R.menu.fragment_dragdraw, menu);
		}
		
		@SuppressLint("NewApi")
		@Override
		public boolean onOptionsItemSelected(MenuItem item) {
			final BoxDrawingController bdc = 
					(BoxDrawingController)getActivity().findViewById(R.id.boxDrawingView);
			bdc.SetGoRemoteService(mGoRemoteService);

			switch (item.getItemId()) {
			case R.id.menu_start:
				// Register this device with a signalling server so that other
				// peers can find it.
				GoNative.registerAsPeer();
				item.setEnabled(false);
				return true;
			case R.id.menu_join:
				// Join with any peer that has already registered itself with
				// the signalling server.
				GoNative.connectPeer();
				// Sync my state with the remote peer.
				bdc.RemoteSync();
				item.setEnabled(false);
				return true;
			default:
				return super.onOptionsItemSelected(item);
			}
		}
	}

	@Override
	public Fragment createFragment() {
		return new DragAndDrawFragment();
	}
}
