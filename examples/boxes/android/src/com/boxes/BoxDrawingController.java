package com.boxes;

import java.util.ArrayList;
import android.content.Context;
import android.graphics.Canvas;
import android.graphics.Color;
import android.graphics.Paint;
import android.graphics.PointF;
import android.os.Bundle;
import android.os.Handler;
import android.os.Parcelable;
import android.util.AttributeSet;
import android.view.MotionEvent;
import android.view.View;

/*
 * BoxDrawingController is the controller for our View. It handles touch events
 * and updates the model as needed. If peer sync has been activated the controller
 * is also responsible for communicating the changes to the remote device through
 * GoRemoteService.
 */
public class BoxDrawingController extends View {
	public static final String PARCELKEY = "BoxViewParcel";
	
	private Box mCurrentBox;
	private ArrayList<Box> mBoxes = new ArrayList<Box>();
	private GoRemoteService mGoRemoteService;
	
	// Color codes for the box, box boundary-line and the background canvas
	private Paint mBoxPaint;
	private Paint mLinePaint;
	private Paint mBackgroundPaint;
	
	// This method is registered with the go-shared library through JNI. When the
	// go shared-library receives an update from a remote-device it notifies the
	// application via this callback.
	public void AddBox(String boxId, float oX, float oY, float cX, float cY) {
	    final Box box = new Box(boxId, new PointF(oX, oY), new PointF(cX, cY));
	    // The View can only be changed in the UI thread. 
	    post(new Runnable() {
	    	@Override
	    	public void run() {
	    		AddRemoteBox(box);
	    	}
	    });
	}
	
	public boolean onTouchEvent(MotionEvent event) {
		final PointF current = new PointF(event.getX(), event.getY());
		
		switch (event.getAction()) {
		case MotionEvent.ACTION_DOWN:
			mCurrentBox = new Box(current);
			mBoxes.add(mCurrentBox);
			break;
		case MotionEvent.ACTION_MOVE:
			if (mCurrentBox != null) {
				mCurrentBox.setCurrent(current);
				// Send the box over to any peer device.
				RemoteDraw(mCurrentBox);
				// View has changed, re-draw it.
				invalidate();
			}
			break;
		case MotionEvent.ACTION_UP:
			mCurrentBox = null;
			break;
		}
		return true;
	}
	
	public BoxDrawingController(Context context) {
		this(context, null);
	}
	
	public BoxDrawingController(Context context, AttributeSet attrs) {
		super(context, attrs);
		this.setSaveEnabled(true);
		
		mBoxPaint = new Paint();
		mBoxPaint.setColor(Color.GREEN);
		
		mLinePaint = new Paint();
		mLinePaint.setStrokeWidth(4f);
		mLinePaint.setColor(Color.BLACK);
		
		mBackgroundPaint = new Paint();
		mBackgroundPaint.setColor(0xfff8efe0);
		
		// Register the AddBox method with the shared-libary so that it knows
		// which callback to use.
		GoNative.registerAddBox(this);
	}
	
	// SetGoRemoteService sets the service that uses an underlying
	// go shared-library to talk to other devices with Veyron.
	void SetGoRemoteService(GoRemoteService goService) {
		mGoRemoteService = goService;
	}

	// We got a remote box from another peer device. Add it to my current
	// state and refresh the view. 
	void AddRemoteBox(Box newBox) {
		for (Box box : mBoxes) {
			if (newBox.getBoxId().equals(box.getBoxId())) {
				// The box already exists. We just need to update its
				// coordinates.
				box.setOrigin(newBox.getOrigin());
				box.setCurrent(newBox.getCurrent());
				invalidate();
				return;
			}
		}
		mBoxes.add(newBox);
		// refresh the view.
		invalidate();
	}
	
	// Need to send over my entire current state to the
	// remote device.
	public void RemoteSync() {
		if (mGoRemoteService != null) {
			for (Box box : mBoxes) {
				mGoRemoteService.RemoteDraw(box);
			}
		}
	}
	
	// Send box information over to any peer device. 
	public void RemoteDraw(Box box) {
		if (mGoRemoteService != null) {
			mGoRemoteService.RemoteDraw(box);
		}
	}
	
	@Override
	protected void onDraw(Canvas canvas) {
		canvas.drawPaint(mBackgroundPaint);
		for (Box box : mBoxes) {
			float left = Math.min(box.getOrigin().x, box.getCurrent().x);
			float right = Math.max(box.getOrigin().x, box.getCurrent().x);
			float top = Math.min(box.getOrigin().y, box.getCurrent().y);
			float bottom = Math.max(box.getOrigin().y, box.getCurrent().y);
			canvas.drawRect(left,  top, right, bottom, mBoxPaint);
	        canvas.drawLine(box.getOrigin().x, box.getOrigin().y, box.getCurrent().x, box.getCurrent().y, mLinePaint);
	        canvas.drawLine(box.getCurrent().x, box.getOrigin().y, box.getOrigin().x, box.getCurrent().y, mLinePaint);	        
		}
	}
	
	@Override
	public Parcelable onSaveInstanceState() {
		Parcelable p = super.onSaveInstanceState();
		Bundle bundle = new Bundle();
		for (Box box : mBoxes) {
			bundle.putBundle(box.getBoxId(), box.toBundle());
		}
	    bundle.putParcelable(PARCELKEY, p);
	    return (Parcelable)bundle;
	}

	@Override
	public void onRestoreInstanceState(Parcelable state) {
		Bundle bundle = (Bundle)state;
		super.onRestoreInstanceState(bundle.getParcelable(PARCELKEY));
	    for (String s : bundle.keySet()) {
	    	if (s == PARCELKEY) continue;
	        mBoxes.add(new Box(bundle.getBundle(s)));
	    }
	}
}
